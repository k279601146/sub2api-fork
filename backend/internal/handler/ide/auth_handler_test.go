//go:build unit

package ide

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type ideAuthUserRepoStub struct {
	service.UserRepository
	users   map[int64]*service.User
	updated []*service.User
}

func (r *ideAuthUserRepoStub) GetByID(_ context.Context, id int64) (*service.User, error) {
	user := r.users[id]
	if user == nil {
		return nil, service.ErrUserNotFound
	}
	return user, nil
}

func (r *ideAuthUserRepoStub) Update(_ context.Context, user *service.User) error {
	r.updated = append(r.updated, user)
	r.users[user.ID] = user
	return nil
}

func (r *ideAuthUserRepoStub) GetUserAvatar(_ context.Context, _ int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (r *ideAuthUserRepoStub) UpdateUserLastActiveAt(_ context.Context, _ int64, _ time.Time) error {
	return nil
}

func newIDEAuthTestServices(user *service.User) (*AuthHandler, *service.AuthService, *ideAuthUserRepoStub) {
	cfg := &config.Config{}
	cfg.JWT.Secret = "test-jwt-secret-32bytes-long!!!"
	cfg.JWT.AccessTokenExpireMinutes = 60

	repo := &ideAuthUserRepoStub{users: map[int64]*service.User{user.ID: user}}
	authSvc := service.NewAuthService(nil, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil)
	userSvc := service.NewUserService(repo, nil, nil, nil)
	return NewAuthHandler(authSvc, userSvc), authSvc, repo
}

func TestTokenExchangesWebJWTForIDEToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	user := &service.User{
		ID:                   42,
		Email:                "ide@example.com",
		Role:                 service.RoleUser,
		Status:               service.StatusActive,
		Concurrency:          5,
		TokenVersion:         7,
		TokenVersionResolved: true,
	}
	handler, authSvc, _ := newIDEAuthTestServices(user)
	resetIDEAuthMemoryForTest()

	webToken, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	body := bytes.NewBufferString(`{"access_token":"` + webToken + `","client_version":"1.2.3"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/ide/auth/token", body)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Token(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var envelope struct {
		Code int `json:"code"`
		Data struct {
			AccessToken   string `json:"access_token"`
			TokenType     string `json:"token_type"`
			ExpiresIn     int    `json:"expires_in"`
			ClientID      string `json:"client_id"`
			ClientVersion string `json:"client_version"`
			SessionID     string `json:"session_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)
	require.NotEmpty(t, envelope.Data.AccessToken)
	require.Equal(t, "Bearer", envelope.Data.TokenType)
	require.Equal(t, 2592000, envelope.Data.ExpiresIn)
	require.Equal(t, defaultIDEClientID, envelope.Data.ClientID)
	require.Equal(t, "1.2.3", envelope.Data.ClientVersion)
	require.NotEmpty(t, envelope.Data.SessionID)

	claims, err := authSvc.ValidateToken(envelope.Data.AccessToken)
	require.NoError(t, err)
	require.Equal(t, int64(42), claims.UserID)
	require.NotContains(t, recorder.Body.String(), "refresh_token")
}

func TestPKCEAuthorizeCallbackAndTokenFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetIDEAuthMemoryForTest()
	user := &service.User{
		ID:                   77,
		Email:                "pkce@example.com",
		Role:                 service.RoleUser,
		Status:               service.StatusActive,
		Concurrency:          5,
		TokenVersion:         2,
		TokenVersionResolved: true,
	}
	handler, authSvc, _ := newIDEAuthTestServices(user)

	verifier := "this-is-a-test-code-verifier"
	authorizeRecorder := httptest.NewRecorder()
	authorizeCtx, _ := gin.CreateTestContext(authorizeRecorder)
	authorizeReq := httptest.NewRequest(
		http.MethodGet,
		"/ide/auth/authorize?redirect_uri=myide://callback&response_mode=json&code_challenge_method=S256&code_challenge="+url.QueryEscape(computePKCEChallenge(verifier)),
		nil,
	)
	authorizeReq.Header.Set("Accept", "application/json")
	authorizeCtx.Request = authorizeReq

	handler.Authorize(authorizeCtx)

	require.Equal(t, http.StatusOK, authorizeRecorder.Code)
	var authorizeEnvelope struct {
		Code int `json:"code"`
		Data struct {
			State string `json:"state"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(authorizeRecorder.Body.Bytes(), &authorizeEnvelope))
	require.Equal(t, 0, authorizeEnvelope.Code)
	require.NotEmpty(t, authorizeEnvelope.Data.State)

	webToken, err := authSvc.GenerateToken(user)
	require.NoError(t, err)
	callbackRecorder := httptest.NewRecorder()
	callbackCtx, _ := gin.CreateTestContext(callbackRecorder)
	callbackReq := httptest.NewRequest(http.MethodGet, "/ide/auth/callback?state="+url.QueryEscape(authorizeEnvelope.Data.State), nil)
	callbackReq.Header.Set("Authorization", "Bearer "+webToken)
	callbackCtx.Request = callbackReq

	handler.Callback(callbackCtx)

	require.Equal(t, http.StatusFound, callbackRecorder.Code)
	redirectLocation := callbackRecorder.Header().Get("Location")
	parsedRedirect, err := url.Parse(redirectLocation)
	require.NoError(t, err)
	require.Equal(t, "myide", parsedRedirect.Scheme)
	require.Equal(t, "callback", parsedRedirect.Host)
	code := parsedRedirect.Query().Get("code")
	require.NotEmpty(t, code)
	require.Equal(t, authorizeEnvelope.Data.State, parsedRedirect.Query().Get("state"))

	tokenBody := bytes.NewBufferString(`{"code":"` + code + `","code_verifier":"` + verifier + `","client_version":"2.0.0","platform":"win32-x64","device_id":"test-device"}`)
	tokenRecorder := httptest.NewRecorder()
	tokenCtx, _ := gin.CreateTestContext(tokenRecorder)
	tokenCtx.Request = httptest.NewRequest(http.MethodPost, "/ide/auth/token", tokenBody)
	tokenCtx.Request.Header.Set("Content-Type", "application/json")

	handler.Token(tokenCtx)

	require.Equal(t, http.StatusOK, tokenRecorder.Code)
	var tokenEnvelope struct {
		Code int `json:"code"`
		Data struct {
			AccessToken string `json:"access_token"`
			SessionID   string `json:"session_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(tokenRecorder.Body.Bytes(), &tokenEnvelope))
	require.Equal(t, 0, tokenEnvelope.Code)
	require.NotEmpty(t, tokenEnvelope.Data.AccessToken)
	require.NotEmpty(t, tokenEnvelope.Data.SessionID)
}

func TestPKCEAuthorizeApproveAndTokenFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetIDEAuthMemoryForTest()
	user := &service.User{
		ID:                   78,
		Email:                "approve@example.com",
		Role:                 service.RoleUser,
		Status:               service.StatusActive,
		Concurrency:          5,
		TokenVersion:         2,
		TokenVersionResolved: true,
	}
	handler, authSvc, repo := newIDEAuthTestServices(user)
	jwtAuth := servermiddleware.NewJWTAuthMiddleware(authSvc, service.NewUserService(repo, nil, nil, nil))

	verifier := "this-is-another-test-code-verifier"
	authorizeRecorder := httptest.NewRecorder()
	authorizeCtx, _ := gin.CreateTestContext(authorizeRecorder)
	authorizeReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/ide/auth/authorize?redirect_uri=http%3A%2F%2F127.0.0.1%3A9456%2Fcallback&response_mode=json&code_challenge_method=S256&client_id=t3code-desktop&code_challenge="+url.QueryEscape(computePKCEChallenge(verifier)),
		nil,
	)
	authorizeReq.Header.Set("Accept", "application/json")
	authorizeCtx.Request = authorizeReq

	handler.Authorize(authorizeCtx)

	require.Equal(t, http.StatusOK, authorizeRecorder.Code)
	var authorizeEnvelope struct {
		Code int `json:"code"`
		Data struct {
			State       string `json:"state"`
			RedirectURI string `json:"redirect_uri"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(authorizeRecorder.Body.Bytes(), &authorizeEnvelope))
	require.Equal(t, 0, authorizeEnvelope.Code)
	require.NotEmpty(t, authorizeEnvelope.Data.State)
	require.Equal(t, "http://127.0.0.1:9456/callback", authorizeEnvelope.Data.RedirectURI)

	webToken, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	router := gin.New()
	router.POST("/api/v1/ide/auth/approve", gin.HandlerFunc(jwtAuth), handler.Approve)

	approveRecorder := httptest.NewRecorder()
	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/ide/auth/approve", bytes.NewBufferString(`{"state":"`+authorizeEnvelope.Data.State+`"}`))
	approveReq.Header.Set("Authorization", "Bearer "+webToken)
	approveReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(approveRecorder, approveReq)

	require.Equal(t, http.StatusOK, approveRecorder.Code)
	var approveEnvelope struct {
		Code int `json:"code"`
		Data struct {
			RedirectURL string `json:"redirect_url"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(approveRecorder.Body.Bytes(), &approveEnvelope))
	require.Equal(t, 0, approveEnvelope.Code)
	parsedRedirect, err := url.Parse(approveEnvelope.Data.RedirectURL)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", parsedRedirect.Hostname())
	code := parsedRedirect.Query().Get("code")
	require.NotEmpty(t, code)

	tokenBody := bytes.NewBufferString(`{"code":"` + code + `","code_verifier":"` + verifier + `","client_id":"t3code-desktop","client_version":"2.0.0"}`)
	tokenRecorder := httptest.NewRecorder()
	tokenCtx, _ := gin.CreateTestContext(tokenRecorder)
	tokenCtx.Request = httptest.NewRequest(http.MethodPost, "/ide/auth/token", tokenBody)
	tokenCtx.Request.Header.Set("Content-Type", "application/json")

	handler.Token(tokenCtx)

	require.Equal(t, http.StatusOK, tokenRecorder.Code)
	require.Contains(t, tokenRecorder.Body.String(), "t3code-desktop")
}

func TestMeAndRevokeUseIDEToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetIDEAuthMemoryForTest()
	user := &service.User{
		ID:                   51,
		Email:                "revoke@example.com",
		Role:                 service.RoleUser,
		Status:               service.StatusActive,
		Concurrency:          5,
		TokenVersion:         3,
		TokenVersionResolved: true,
	}
	handler, authSvc, repo := newIDEAuthTestServices(user)
	jwtAuth := servermiddleware.NewJWTAuthMiddleware(authSvc, service.NewUserService(repo, nil, nil, nil))

	token, err := authSvc.GenerateToken(user)
	require.NoError(t, err)

	router := gin.New()
	router.Use(gin.HandlerFunc(jwtAuth))
	router.GET("/ide/auth/me", handler.Me)
	router.POST("/ide/auth/revoke", handler.Revoke)

	meRecorder := httptest.NewRecorder()
	meReq := httptest.NewRequest(http.MethodGet, "/ide/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(meRecorder, meReq)
	require.Equal(t, http.StatusOK, meRecorder.Code)
	require.Contains(t, meRecorder.Body.String(), "revoke@example.com")

	revokeRecorder := httptest.NewRecorder()
	revokeReq := httptest.NewRequest(http.MethodPost, "/ide/auth/revoke", nil)
	revokeReq.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(revokeRecorder, revokeReq)
	require.Equal(t, http.StatusOK, revokeRecorder.Code)
	require.Len(t, repo.updated, 1)
	require.Equal(t, int64(4), repo.users[51].TokenVersion)
}

func TestAdminRevokeSessionInvalidatesUserTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetIDEAuthMemoryForTest()
	user := &service.User{
		ID:                   88,
		Email:                "admin-revoke@example.com",
		Role:                 service.RoleUser,
		Status:               service.StatusActive,
		Concurrency:          5,
		TokenVersion:         1,
		TokenVersionResolved: true,
	}
	handler, authSvc, repo := newIDEAuthTestServices(user)

	webToken, err := authSvc.GenerateToken(user)
	require.NoError(t, err)
	body := bytes.NewBufferString(`{"access_token":"` + webToken + `","client_version":"1.0.0"}`)
	tokenRecorder := httptest.NewRecorder()
	tokenCtx, _ := gin.CreateTestContext(tokenRecorder)
	tokenCtx.Request = httptest.NewRequest(http.MethodPost, "/ide/auth/token", body)
	tokenCtx.Request.Header.Set("Content-Type", "application/json")
	handler.Token(tokenCtx)
	require.Equal(t, http.StatusOK, tokenRecorder.Code)

	var tokenEnvelope struct {
		Data struct {
			SessionID string `json:"session_id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(tokenRecorder.Body.Bytes(), &tokenEnvelope))
	require.NotEmpty(t, tokenEnvelope.Data.SessionID)

	revokeRecorder := httptest.NewRecorder()
	revokeCtx, _ := gin.CreateTestContext(revokeRecorder)
	revokeCtx.Params = gin.Params{{Key: "id", Value: tokenEnvelope.Data.SessionID}}
	revokeCtx.Request = httptest.NewRequest(http.MethodPost, "/admin/ide/sessions/"+tokenEnvelope.Data.SessionID+"/revoke", nil)
	handler.RevokeSession(revokeCtx)

	require.Equal(t, http.StatusOK, revokeRecorder.Code)
	require.Len(t, repo.updated, 1)
	require.Equal(t, int64(2), repo.users[88].TokenVersion)
}

func resetIDEAuthMemoryForTest() {
	ideAuthMemory.Lock()
	defer ideAuthMemory.Unlock()
	ideAuthMemory.states = map[string]ideAuthStateRecord{}
	ideAuthMemory.codes = map[string]ideAuthCodeRecord{}
	ideAuthMemory.sessions = map[string]IDESessionRecord{}
}

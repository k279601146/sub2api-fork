//go:build unit

package ide

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)
	require.NotEmpty(t, envelope.Data.AccessToken)
	require.Equal(t, "Bearer", envelope.Data.TokenType)
	require.Equal(t, 3600, envelope.Data.ExpiresIn)
	require.Equal(t, defaultIDEClientID, envelope.Data.ClientID)
	require.Equal(t, "1.2.3", envelope.Data.ClientVersion)

	claims, err := authSvc.ValidateToken(envelope.Data.AccessToken)
	require.NoError(t, err)
	require.Equal(t, int64(42), claims.UserID)
	require.NotContains(t, recorder.Body.String(), "refresh_token")
}

func TestMeAndRevokeUseIDEToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
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

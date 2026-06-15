package ide

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/idesession"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const (
	defaultIDEClientID = "myide-desktop"
	ideTokenType       = "Bearer"
	ideAuthStateTTL    = 5 * time.Minute
	ideAuthCodeTTL     = time.Minute
)

// AuthHandler exposes authentication endpoints designed for desktop IDE clients.
type AuthHandler struct {
	authService *service.AuthService
	userService *service.UserService
}

type ideAuthStateRecord struct {
	CodeChallenge string
	RedirectURI   string
	ClientID      string
	ExpiresAt     time.Time
}

type ideAuthCodeRecord struct {
	UserID        int64
	CodeChallenge string
	ExpiresAt     time.Time
}

type IDESessionRecord struct {
	ID            string    `json:"id"`
	UserID        int64     `json:"user_id"`
	UserEmail     string    `json:"user_email,omitempty"`
	TokenHash     string    `json:"-"`
	ClientID      string    `json:"client_id"`
	ClientVersion string    `json:"client_version,omitempty"`
	Platform      string    `json:"platform,omitempty"`
	DeviceID      string    `json:"device_id,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
	LastUsedAt    time.Time `json:"last_used_at"`
	CreatedAt     time.Time `json:"created_at"`
	Revoked       bool      `json:"revoked"`
	RevokeReason  string    `json:"revoke_reason,omitempty"`
}

var ideAuthMemory = struct {
	sync.Mutex
	states   map[string]ideAuthStateRecord
	codes    map[string]ideAuthCodeRecord
	sessions map[string]IDESessionRecord
}{
	states:   map[string]ideAuthStateRecord{},
	codes:    map[string]ideAuthCodeRecord{},
	sessions: map[string]IDESessionRecord{},
}

func NewAuthHandler(authService *service.AuthService, userService *service.UserService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
		userService: userService,
	}
}

func (h *AuthHandler) EntClient() *dbent.Client {
	if h == nil || h.authService == nil {
		return nil
	}
	return h.authService.EntClient()
}

func (h *AuthHandler) UserIDFromAuthorizationHeader(ctx context.Context, authHeader string) (int64, bool) {
	if h == nil {
		return 0, false
	}
	tokenString := bearerTokenFromHeader(authHeader)
	if tokenString == "" {
		return 0, false
	}
	user, err := h.userFromAccessToken(ctx, tokenString)
	if err != nil {
		return 0, false
	}
	return user.ID, true
}

type TokenRequest struct {
	GrantType     string `json:"grant_type"`
	AccessToken   string `json:"access_token"`
	Code          string `json:"code"`
	CodeVerifier  string `json:"code_verifier"`
	ClientID      string `json:"client_id"`
	ClientVersion string `json:"client_version"`
	Platform      string `json:"platform"`
	DeviceID      string `json:"device_id"`
}

type TokenResponse struct {
	AccessToken   string    `json:"access_token"`
	TokenType     string    `json:"token_type"`
	ExpiresIn     int       `json:"expires_in"`
	ClientID      string    `json:"client_id"`
	ClientVersion string    `json:"client_version,omitempty"`
	SessionID     string    `json:"session_id,omitempty"`
	User          *dto.User `json:"user"`
}

type MeResponse struct {
	User    *dto.User         `json:"user"`
	Session *IDESessionRecord `json:"session,omitempty"`
}

type RevokeResponse struct {
	Message string `json:"message"`
}

type ApproveRequest struct {
	State string `json:"state"`
}

type ApproveResponse struct {
	RedirectURL string    `json:"redirect_url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type AuthorizeResponse struct {
	State       string    `json:"state"`
	LoginURL    string    `json:"login_url"`
	RedirectURI string    `json:"redirect_uri"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type AdminIDESessionsResponse struct {
	Items []IDESessionRecord `json:"items"`
	Total int                `json:"total"`
}

type AdminIDEStatsResponse struct {
	ActiveSessions  int `json:"active_sessions"`
	RevokedSessions int `json:"revoked_sessions"`
	TotalSessions   int `json:"total_sessions"`
}

// Authorize starts the IDE PKCE login flow.
//
// The route stores the PKCE challenge and redirects the user to the regular web
// login page carrying ide_state. After login, the web layer can call
// /ide/auth/callback with that state and a valid web JWT to produce an IDE code.
func (h *AuthHandler) Authorize(c *gin.Context) {
	codeChallenge := strings.TrimSpace(c.Query("code_challenge"))
	redirectURI := strings.TrimSpace(c.Query("redirect_uri"))
	clientID := strings.TrimSpace(c.Query("client_id"))
	if clientID == "" {
		clientID = defaultIDEClientID
	}

	if codeChallenge == "" || redirectURI == "" {
		response.BadRequest(c, "code_challenge and redirect_uri are required")
		return
	}
	if method := strings.TrimSpace(c.Query("code_challenge_method")); method != "" && !strings.EqualFold(method, "S256") {
		response.BadRequest(c, "only S256 code_challenge_method is supported")
		return
	}
	if !isAllowedRedirectURI(redirectURI) {
		response.BadRequest(c, "invalid redirect_uri")
		return
	}

	if !wantsJSON(c) {
		loginURL := "/login?redirect=" + url.QueryEscape(c.Request.URL.RequestURI())
		c.Redirect(http.StatusFound, loginURL)
		return
	}

	state, err := secureRandomToken(32)
	if err != nil {
		response.InternalError(c, "failed to create IDE auth state")
		return
	}
	expiresAt := time.Now().UTC().Add(ideAuthStateTTL)
	ideAuthMemory.Lock()
	cleanupIDEAuthMemoryLocked(time.Now().UTC())
	ideAuthMemory.states[state] = ideAuthStateRecord{
		CodeChallenge: codeChallenge,
		RedirectURI:   redirectURI,
		ClientID:      clientID,
		ExpiresAt:     expiresAt,
	}
	ideAuthMemory.Unlock()

	loginURL := "/login?ide_state=" + url.QueryEscape(state)
	response.Success(c, AuthorizeResponse{
		State:       state,
		LoginURL:    loginURL,
		RedirectURI: redirectURI,
		ExpiresAt:   expiresAt,
	})
}

// Callback completes the browser-authenticated side of the PKCE flow.
func (h *AuthHandler) Callback(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	if state == "" {
		response.BadRequest(c, "state is required")
		return
	}

	ideAuthMemory.Lock()
	cleanupIDEAuthMemoryLocked(time.Now().UTC())
	authState, ok := ideAuthMemory.states[state]
	if ok {
		delete(ideAuthMemory.states, state)
	}
	ideAuthMemory.Unlock()
	if !ok {
		response.ErrorFrom(c, infraerrors.Unauthorized("IDE_AUTH_STATE_INVALID", "invalid or expired IDE auth state"))
		return
	}

	tokenString := bearerTokenFromHeader(c.GetHeader("Authorization"))
	if tokenString == "" {
		tokenString = strings.TrimSpace(c.Query("access_token"))
	}
	user, err := h.userFromAccessToken(c.Request.Context(), tokenString)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	code, err := secureRandomToken(32)
	if err != nil {
		response.InternalError(c, "failed to create IDE auth code")
		return
	}
	expiresAt := time.Now().UTC().Add(ideAuthCodeTTL)
	ideAuthMemory.Lock()
	ideAuthMemory.codes[code] = ideAuthCodeRecord{
		UserID:        user.ID,
		CodeChallenge: authState.CodeChallenge,
		ExpiresAt:     expiresAt,
	}
	ideAuthMemory.Unlock()

	redirectURL, err := appendQuery(authState.RedirectURI, map[string]string{
		"code":  code,
		"state": state,
	})
	if err != nil {
		response.InternalError(c, "failed to build IDE redirect URL")
		return
	}
	c.Redirect(http.StatusFound, redirectURL)
}

// Approve completes the browser side of the PKCE flow for the SPA consent page.
func (h *AuthHandler) Approve(c *gin.Context) {
	var req ApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "state is required")
		return
	}
	state := strings.TrimSpace(req.State)
	if state == "" {
		response.BadRequest(c, "state is required")
		return
	}

	ideAuthMemory.Lock()
	cleanupIDEAuthMemoryLocked(time.Now().UTC())
	authState, ok := ideAuthMemory.states[state]
	if ok {
		delete(ideAuthMemory.states, state)
	}
	ideAuthMemory.Unlock()
	if !ok {
		response.ErrorFrom(c, infraerrors.Unauthorized("IDE_AUTH_STATE_INVALID", "invalid or expired IDE auth state"))
		return
	}

	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.ErrorFrom(c, infraerrors.Unauthorized("IDE_AUTH_REQUIRED", "IDE authentication is required"))
		return
	}

	user, err := h.userService.GetByID(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, infraerrors.Unauthorized("IDE_USER_NOT_FOUND", "user not found").WithCause(err))
		return
	}
	if !user.IsActive() {
		response.ErrorFrom(c, service.ErrUserNotActive)
		return
	}

	code, err := secureRandomToken(32)
	if err != nil {
		response.InternalError(c, "failed to create IDE auth code")
		return
	}
	expiresAt := time.Now().UTC().Add(ideAuthCodeTTL)
	ideAuthMemory.Lock()
	ideAuthMemory.codes[code] = ideAuthCodeRecord{
		UserID:        user.ID,
		CodeChallenge: authState.CodeChallenge,
		ExpiresAt:     expiresAt,
	}
	ideAuthMemory.Unlock()

	redirectURL, err := appendQuery(authState.RedirectURI, map[string]string{
		"code":  code,
		"state": state,
	})
	if err != nil {
		response.InternalError(c, "failed to build IDE redirect URL")
		return
	}
	response.Success(c, ApproveResponse{
		RedirectURL: redirectURL,
		ExpiresAt:   expiresAt,
	})
}

// AuthorizeDev2User completes the browser consent side of the IDE PKCE flow
// after dev2 has authenticated the user and collected consent.
func (h *AuthHandler) AuthorizeDev2User(ctx context.Context, user *service.User, codeChallenge, redirectURI, clientID string) (ApproveResponse, error) {
	codeChallenge = strings.TrimSpace(codeChallenge)
	redirectURI = strings.TrimSpace(redirectURI)
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = defaultIDEClientID
	}
	if user == nil || user.ID <= 0 {
		return ApproveResponse{}, infraerrors.Unauthorized("IDE_USER_NOT_FOUND", "user not found")
	}
	if !user.IsActive() {
		return ApproveResponse{}, service.ErrUserNotActive
	}
	if codeChallenge == "" || redirectURI == "" {
		return ApproveResponse{}, infraerrors.BadRequest("IDE_AUTH_REQUEST_INVALID", "code_challenge and redirect_uri are required")
	}
	if !isAllowedRedirectURI(redirectURI) {
		return ApproveResponse{}, infraerrors.BadRequest("IDE_REDIRECT_URI_INVALID", "invalid redirect_uri")
	}

	code, err := secureRandomToken(32)
	if err != nil {
		return ApproveResponse{}, err
	}
	state, err := secureRandomToken(18)
	if err != nil {
		return ApproveResponse{}, err
	}
	expiresAt := time.Now().UTC().Add(ideAuthCodeTTL)
	ideAuthMemory.Lock()
	cleanupIDEAuthMemoryLocked(time.Now().UTC())
	ideAuthMemory.codes[code] = ideAuthCodeRecord{
		UserID:        user.ID,
		CodeChallenge: codeChallenge,
		ExpiresAt:     expiresAt,
	}
	ideAuthMemory.Unlock()

	redirectURL, err := appendQuery(redirectURI, map[string]string{
		"code":  code,
		"state": state,
	})
	if err != nil {
		return ApproveResponse{}, err
	}
	return ApproveResponse{RedirectURL: redirectURL, ExpiresAt: expiresAt}, nil
}

// Token exchanges an already-authenticated web JWT for an IDE JWT.
//
// This is the first desktop bridge for local/commercial IDE integration. The
// browser PKCE callback can later call the same issuance path after producing a
// user-authenticated JWT.
func (h *AuthHandler) Token(c *gin.Context) {
	var req TokenRequest
	_ = c.ShouldBindJSON(&req)

	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		clientID = defaultIDEClientID
	}

	if strings.TrimSpace(req.Code) != "" || strings.TrimSpace(req.CodeVerifier) != "" {
		h.tokenFromPKCECode(c, req, clientID)
		return
	}

	tokenString := strings.TrimSpace(req.AccessToken)
	if tokenString == "" {
		tokenString = bearerTokenFromHeader(c.GetHeader("Authorization"))
	}
	if tokenString == "" {
		response.ErrorFrom(c, infraerrors.Unauthorized("IDE_ACCESS_TOKEN_REQUIRED", "access token is required"))
		return
	}

	user, err := h.userFromAccessToken(c.Request.Context(), tokenString)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	ideToken, err := h.authService.GenerateIDEToken(user)
	if err != nil {
		response.InternalError(c, "Failed to generate IDE token")
		return
	}

	sessionID := h.recordIDESession(c.Request.Context(), user, ideToken, clientID, req.ClientVersion, req.Platform, req.DeviceID, h.authService.GetIDETokenExpiresIn())
	response.Success(c, TokenResponse{
		AccessToken:   ideToken,
		TokenType:     ideTokenType,
		ExpiresIn:     h.authService.GetIDETokenExpiresIn(),
		ClientID:      clientID,
		ClientVersion: strings.TrimSpace(req.ClientVersion),
		SessionID:     sessionID,
		User:          dto.UserFromService(user),
	})
}

func (h *AuthHandler) Me(c *gin.Context) {
	user, err := h.authenticatedUser(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, MeResponse{
		User:    dto.UserFromService(user),
		Session: h.touchIDESession(c.Request.Context(), bearerTokenFromHeader(c.GetHeader("Authorization"))),
	})
}

// Revoke invalidates all currently-issued user JWTs by bumping token_version.
// A persisted IDE session table can narrow this to a single device later.
func (h *AuthHandler) Revoke(c *gin.Context) {
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.ErrorFrom(c, infraerrors.Unauthorized("IDE_AUTH_REQUIRED", "IDE authentication is required"))
		return
	}

	if err := h.authService.RevokeAllUserTokens(c.Request.Context(), subject.UserID); err != nil {
		response.InternalError(c, "Failed to revoke IDE sessions")
		return
	}
	revokeIDESessionsForUser(subject.UserID, "user_logout")

	response.Success(c, RevokeResponse{Message: "IDE sessions revoked"})
}

func (h *AuthHandler) ListSessions(c *gin.Context) {
	if client := h.EntClient(); client != nil {
		rows, err := client.IDESession.Query().
			Order(dbent.Desc(idesession.FieldLastUsedAt)).
			Limit(500).
			All(c.Request.Context())
		if err == nil {
			items := make([]IDESessionRecord, 0, len(rows))
			for _, row := range rows {
				items = append(items, ideSessionRecordFromEnt(row))
			}
			response.Success(c, AdminIDESessionsResponse{Items: items, Total: len(items)})
			return
		}
	}

	ideAuthMemory.Lock()
	cleanupIDEAuthMemoryLocked(time.Now().UTC())
	items := make([]IDESessionRecord, 0, len(ideAuthMemory.sessions))
	for _, session := range ideAuthMemory.sessions {
		items = append(items, session)
	}
	ideAuthMemory.Unlock()

	response.Success(c, AdminIDESessionsResponse{
		Items: items,
		Total: len(items),
	})
}

func (h *AuthHandler) RevokeSession(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		response.BadRequest(c, "session id is required")
		return
	}

	if client := h.EntClient(); client != nil {
		session, err := client.IDESession.Query().
			Where(idesession.SessionID(sessionID)).
			Only(c.Request.Context())
		if err == nil {
			if _, err := client.IDESession.UpdateOne(session).
				SetRevoked(true).
				SetRevokeReason("admin_revoke").
				Save(c.Request.Context()); err != nil {
				response.InternalError(c, "Failed to revoke IDE session")
				return
			}
			if err := h.authService.RevokeAllUserTokens(c.Request.Context(), session.UserID); err != nil {
				response.InternalError(c, "Failed to revoke IDE session tokens")
				return
			}
			response.Success(c, RevokeResponse{Message: "IDE session revoked"})
			return
		}
		if !dbent.IsNotFound(err) {
			response.InternalError(c, "Failed to load IDE session")
			return
		}
	}

	ideAuthMemory.Lock()
	session, ok := ideAuthMemory.sessions[sessionID]
	if ok {
		session.Revoked = true
		session.RevokeReason = "admin_revoke"
		ideAuthMemory.sessions[sessionID] = session
	}
	ideAuthMemory.Unlock()
	if !ok {
		response.NotFound(c, "IDE session not found")
		return
	}
	if err := h.authService.RevokeAllUserTokens(c.Request.Context(), session.UserID); err != nil {
		response.InternalError(c, "Failed to revoke IDE session tokens")
		return
	}
	response.Success(c, RevokeResponse{Message: "IDE session revoked"})
}

func (h *AuthHandler) Stats(c *gin.Context) {
	if client := h.EntClient(); client != nil {
		total, totalErr := client.IDESession.Query().Count(c.Request.Context())
		revoked, revokedErr := client.IDESession.Query().Where(idesession.Revoked(true)).Count(c.Request.Context())
		if totalErr == nil && revokedErr == nil {
			response.Success(c, AdminIDEStatsResponse{
				ActiveSessions:  total - revoked,
				RevokedSessions: revoked,
				TotalSessions:   total,
			})
			return
		}
	}

	ideAuthMemory.Lock()
	cleanupIDEAuthMemoryLocked(time.Now().UTC())
	stats := AdminIDEStatsResponse{}
	for _, session := range ideAuthMemory.sessions {
		stats.TotalSessions++
		if session.Revoked {
			stats.RevokedSessions++
		} else {
			stats.ActiveSessions++
		}
	}
	ideAuthMemory.Unlock()
	response.Success(c, stats)
}

func (h *AuthHandler) tokenFromPKCECode(c *gin.Context, req TokenRequest, clientID string) {
	code := strings.TrimSpace(req.Code)
	codeVerifier := strings.TrimSpace(req.CodeVerifier)
	if code == "" || codeVerifier == "" {
		response.BadRequest(c, "code and code_verifier are required")
		return
	}

	ideAuthMemory.Lock()
	cleanupIDEAuthMemoryLocked(time.Now().UTC())
	authCode, ok := ideAuthMemory.codes[code]
	if ok {
		delete(ideAuthMemory.codes, code)
	}
	ideAuthMemory.Unlock()
	if !ok {
		response.ErrorFrom(c, infraerrors.Unauthorized("IDE_AUTH_CODE_INVALID", "invalid or expired IDE auth code"))
		return
	}
	if computePKCEChallenge(codeVerifier) != authCode.CodeChallenge {
		response.ErrorFrom(c, infraerrors.Unauthorized("IDE_PKCE_MISMATCH", "code_verifier mismatch"))
		return
	}

	user, err := h.userService.GetByID(c.Request.Context(), authCode.UserID)
	if err != nil {
		response.ErrorFrom(c, infraerrors.Unauthorized("IDE_USER_NOT_FOUND", "user not found").WithCause(err))
		return
	}
	if !user.IsActive() {
		response.ErrorFrom(c, service.ErrUserNotActive)
		return
	}

	ideToken, err := h.authService.GenerateIDEToken(user)
	if err != nil {
		response.InternalError(c, "Failed to generate IDE token")
		return
	}
	sessionID := h.recordIDESession(c.Request.Context(), user, ideToken, clientID, req.ClientVersion, req.Platform, req.DeviceID, h.authService.GetIDETokenExpiresIn())
	response.Success(c, TokenResponse{
		AccessToken:   ideToken,
		TokenType:     ideTokenType,
		ExpiresIn:     h.authService.GetIDETokenExpiresIn(),
		ClientID:      clientID,
		ClientVersion: strings.TrimSpace(req.ClientVersion),
		SessionID:     sessionID,
		User:          dto.UserFromService(user),
	})
}

func (h *AuthHandler) authenticatedUser(c *gin.Context) (*service.User, error) {
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok {
		return nil, infraerrors.Unauthorized("IDE_AUTH_REQUIRED", "IDE authentication is required")
	}

	user, err := h.userService.GetByID(c.Request.Context(), subject.UserID)
	if err != nil {
		return nil, infraerrors.Unauthorized("IDE_USER_NOT_FOUND", "user not found").WithCause(err)
	}
	return user, nil
}

func (h *AuthHandler) userFromAccessToken(ctx context.Context, tokenString string) (*service.User, error) {
	claims, err := h.authService.ValidateToken(tokenString)
	if err != nil {
		if errors.Is(err, service.ErrTokenExpired) {
			return nil, service.ErrTokenExpired
		}
		return nil, service.ErrInvalidToken
	}

	user, err := h.userService.GetByID(ctx, claims.UserID)
	if err != nil {
		return nil, infraerrors.Unauthorized("IDE_USER_NOT_FOUND", "user not found").WithCause(err)
	}
	if !user.IsActive() {
		return nil, service.ErrUserNotActive
	}
	if claims.TokenVersion != user.TokenVersion {
		return nil, service.ErrTokenRevoked
	}

	return user, nil
}

func bearerTokenFromHeader(authHeader string) string {
	parts := strings.SplitN(strings.TrimSpace(authHeader), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func secureRandomToken(byteCount int) (string, error) {
	buffer := make([]byte, byteCount)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func computePKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (h *AuthHandler) recordIDESession(ctx context.Context, user *service.User, token, clientID, clientVersion, platform, deviceID string, expiresIn int) string {
	sessionID, err := secureRandomToken(18)
	if err != nil {
		sessionID = hashToken(token)[:24]
	}
	now := time.Now().UTC()
	record := IDESessionRecord{
		ID:            sessionID,
		UserID:        user.ID,
		UserEmail:     user.Email,
		TokenHash:     hashToken(token),
		ClientID:      strings.TrimSpace(clientID),
		ClientVersion: strings.TrimSpace(clientVersion),
		Platform:      strings.TrimSpace(platform),
		DeviceID:      strings.TrimSpace(deviceID),
		ExpiresAt:     now.Add(time.Duration(expiresIn) * time.Second),
		LastUsedAt:    now,
		CreatedAt:     now,
	}
	if client := h.EntClient(); client != nil {
		if _, err := client.IDESession.Create().
			SetSessionID(record.ID).
			SetUserID(record.UserID).
			SetJwtTokenHash(record.TokenHash).
			SetClientID(record.ClientID).
			SetClientVersion(record.ClientVersion).
			SetPlatform(record.Platform).
			SetDeviceID(record.DeviceID).
			SetExpiresAt(record.ExpiresAt).
			SetLastUsedAt(record.LastUsedAt).
			SetCreatedAt(record.CreatedAt).
			Save(ctx); err == nil {
			return sessionID
		}
	}

	ideAuthMemory.Lock()
	ideAuthMemory.sessions[sessionID] = record
	ideAuthMemory.Unlock()
	return sessionID
}

func (h *AuthHandler) touchIDESession(ctx context.Context, token string) *IDESessionRecord {
	if token == "" {
		return nil
	}
	tokenHash := hashToken(token)
	now := time.Now().UTC()
	if client := h.EntClient(); client != nil {
		session, err := client.IDESession.Query().
			Where(idesession.JwtTokenHash(tokenHash)).
			Only(ctx)
		if err == nil {
			_, _ = client.IDESession.UpdateOne(session).SetLastUsedAt(now).Save(ctx)
			session.LastUsedAt = now
			record := ideSessionRecordFromEnt(session)
			return &record
		}
	}

	ideAuthMemory.Lock()
	defer ideAuthMemory.Unlock()
	for id, session := range ideAuthMemory.sessions {
		if session.TokenHash == tokenHash {
			session.LastUsedAt = now
			ideAuthMemory.sessions[id] = session
			copy := session
			return &copy
		}
	}
	return nil
}

func ideSessionRecordFromEnt(session *dbent.IDESession) IDESessionRecord {
	return IDESessionRecord{
		ID:            session.SessionID,
		UserID:        session.UserID,
		TokenHash:     session.JwtTokenHash,
		ClientID:      session.ClientID,
		ClientVersion: session.ClientVersion,
		Platform:      session.Platform,
		DeviceID:      session.DeviceID,
		ExpiresAt:     session.ExpiresAt,
		LastUsedAt:    session.LastUsedAt,
		CreatedAt:     session.CreatedAt,
		Revoked:       session.Revoked,
		RevokeReason:  session.RevokeReason,
	}
}

func revokeIDESessionsForUser(userID int64, reason string) {
	ideAuthMemory.Lock()
	defer ideAuthMemory.Unlock()
	for id, session := range ideAuthMemory.sessions {
		if session.UserID == userID {
			session.Revoked = true
			session.RevokeReason = reason
			ideAuthMemory.sessions[id] = session
		}
	}
}

func cleanupIDEAuthMemoryLocked(now time.Time) {
	for state, record := range ideAuthMemory.states {
		if now.After(record.ExpiresAt) {
			delete(ideAuthMemory.states, state)
		}
	}
	for code, record := range ideAuthMemory.codes {
		if now.After(record.ExpiresAt) {
			delete(ideAuthMemory.codes, code)
		}
	}
}

func isAllowedRedirectURI(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if parsed.Scheme == "myide" && parsed.Host == "callback" {
		return true
	}
	if (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() == "127.0.0.1" && parsed.Path == "/callback" {
		return true
	}
	return false
}

func appendQuery(raw string, values map[string]string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for key, value := range values {
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func wantsJSON(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "application/json") || strings.EqualFold(c.Query("response_mode"), "json")
}

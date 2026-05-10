package ide

import (
	"context"
	"errors"
	"strings"

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
)

// AuthHandler exposes authentication endpoints designed for desktop IDE clients.
type AuthHandler struct {
	authService *service.AuthService
	userService *service.UserService
}

func NewAuthHandler(authService *service.AuthService, userService *service.UserService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
		userService: userService,
	}
}

type TokenRequest struct {
	GrantType     string `json:"grant_type"`
	AccessToken   string `json:"access_token"`
	ClientID      string `json:"client_id"`
	ClientVersion string `json:"client_version"`
}

type TokenResponse struct {
	AccessToken   string    `json:"access_token"`
	TokenType     string    `json:"token_type"`
	ExpiresIn     int       `json:"expires_in"`
	ClientID      string    `json:"client_id"`
	ClientVersion string    `json:"client_version,omitempty"`
	User          *dto.User `json:"user"`
}

type MeResponse struct {
	User *dto.User `json:"user"`
}

type RevokeResponse struct {
	Message string `json:"message"`
}

// Token exchanges an already-authenticated web JWT for an IDE JWT.
//
// This is the first desktop bridge for local/commercial IDE integration. The
// browser PKCE callback can later call the same issuance path after producing a
// user-authenticated JWT.
func (h *AuthHandler) Token(c *gin.Context) {
	var req TokenRequest
	_ = c.ShouldBindJSON(&req)

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

	ideToken, err := h.authService.GenerateToken(user)
	if err != nil {
		response.InternalError(c, "Failed to generate IDE token")
		return
	}

	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		clientID = defaultIDEClientID
	}

	response.Success(c, TokenResponse{
		AccessToken:   ideToken,
		TokenType:     ideTokenType,
		ExpiresIn:     h.authService.GetAccessTokenExpiresIn(),
		ClientID:      clientID,
		ClientVersion: strings.TrimSpace(req.ClientVersion),
		User:          dto.UserFromService(user),
	})
}

func (h *AuthHandler) Me(c *gin.Context) {
	user, err := h.authenticatedUser(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, MeResponse{User: dto.UserFromService(user)})
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

	response.Success(c, RevokeResponse{Message: "IDE sessions revoked"})
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

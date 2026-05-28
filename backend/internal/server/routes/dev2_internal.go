package routes

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"net/mail"
	"os"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
)

const (
	dev2ProviderType = "dev2"
	dev2ProviderKey  = "dev2"
)

type dev2SyncUserRequest struct {
	ExternalID string `json:"external_id"`
	Email      string `json:"email"`
}

type dev2AuthorizeRequest struct {
	ExternalID          string `json:"external_id"`
	Email               string `json:"email"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	RedirectURI         string `json:"redirect_uri"`
	ClientID            string `json:"client_id"`
	ClientVersion       string `json:"client_version"`
	Platform            string `json:"platform"`
	DeviceID            string `json:"device_id"`
}

type dev2UserResponse struct {
	ID         int64   `json:"id"`
	Email      string  `json:"email"`
	ExternalID string  `json:"external_id"`
	Balance    float64 `json:"balance"`
	Status     string  `json:"status"`
}

type dev2BillingSnapshot struct {
	User        dev2UserResponse               `json:"user"`
	Balance     float64                        `json:"balance"`
	Status      string                         `json:"status"`
	Usage       *usagestats.UserDashboardStats `json:"usage"`
	GeneratedAt time.Time                      `json:"generated_at"`
}

// RegisterDev2InternalRoutes exposes service-to-service endpoints used by dev2.
func RegisterDev2InternalRoutes(r *gin.Engine, h *handler.Handlers, cfg *config.Config) {
	slog.Info(
		"registering dev2 internal routes",
		"cfg_secret_configured", cfg != nil && strings.TrimSpace(cfg.Dev2.InternalSecret) != "",
		"viper_secret_configured", strings.TrimSpace(viper.GetString("dev2.internal_secret")) != "",
		"env_secret_configured", strings.TrimSpace(os.Getenv("DEV2_INTERNAL_SECRET")) != "",
	)
	group := r.Group("/internal/dev2")
	group.Use(dev2InternalAuthMiddleware(cfg))
	{
		group.POST("/users/sync", dev2SyncUser(h, cfg))
		group.POST("/ide/auth/authorize", dev2AuthorizeIDE(h, cfg))
		group.GET("/billing/usage", dev2BillingUsage(h, cfg))
		group.POST("/billing/usage/check", dev2BillingUsage(h, cfg))
	}
}

func dev2InternalAuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		secret := strings.TrimSpace(os.Getenv("DEV2_INTERNAL_SECRET"))
		if cfg != nil {
			if configured := strings.TrimSpace(cfg.Dev2.InternalSecret); configured != "" {
				secret = configured
			}
		}
		if secret == "" {
			secret = strings.TrimSpace(viper.GetString("dev2.internal_secret"))
		}
		if secret == "" {
			slog.Warn(
				"dev2 internal secret is not configured",
				"cfg_secret_configured", cfg != nil && strings.TrimSpace(cfg.Dev2.InternalSecret) != "",
				"viper_secret_configured", strings.TrimSpace(viper.GetString("dev2.internal_secret")) != "",
				"env_secret_configured", strings.TrimSpace(os.Getenv("DEV2_INTERNAL_SECRET")) != "",
			)
			response.NotFound(c, "not found")
			c.Abort()
			return
		}
		presented := strings.TrimSpace(c.GetHeader("X-Dev2-Internal-Secret"))
		if presented == "" || subtle.ConstantTimeCompare([]byte(presented), []byte(secret)) != 1 {
			response.Unauthorized(c, "invalid internal secret")
			c.Abort()
			return
		}
		c.Next()
	}
}

func dev2SyncUser(h *handler.Handlers, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req dev2SyncUserRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "invalid request")
			return
		}
		user, err := upsertDev2User(c, h, cfg, req.ExternalID, req.Email)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, dev2UserPayload(user, strings.TrimSpace(req.ExternalID)))
	}
}

func dev2AuthorizeIDE(h *handler.Handlers, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req dev2AuthorizeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "invalid request")
			return
		}
		if method := strings.TrimSpace(req.CodeChallengeMethod); method != "" && !strings.EqualFold(method, "S256") {
			response.BadRequest(c, "only S256 code_challenge_method is supported")
			return
		}
		user, err := upsertDev2User(c, h, cfg, req.ExternalID, req.Email)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		result, err := h.IDEAuth.AuthorizeDev2User(c.Request.Context(), user, req.CodeChallenge, req.RedirectURI, req.ClientID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, result)
	}
}

func dev2BillingUsage(h *handler.Handlers, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		externalID := strings.TrimSpace(c.Query("external_id"))
		email := strings.TrimSpace(c.Query("email"))
		if externalID == "" {
			var req dev2SyncUserRequest
			_ = c.ShouldBindJSON(&req)
			externalID = strings.TrimSpace(req.ExternalID)
			email = strings.TrimSpace(req.Email)
		}
		user, err := upsertDev2User(c, h, cfg, externalID, email)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if h.Usage == nil || h.Usage.UsageService() == nil {
			response.InternalError(c, "usage service is unavailable")
			return
		}
		stats, err := h.Usage.UsageService().GetUserDashboardStats(c.Request.Context(), user.ID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, dev2BillingSnapshot{
			User:        dev2UserPayload(user, externalID),
			Balance:     user.Balance,
			Status:      user.Status,
			Usage:       stats,
			GeneratedAt: time.Now().UTC(),
		})
	}
}

func upsertDev2User(c *gin.Context, h *handler.Handlers, cfg *config.Config, externalID, email string) (*service.User, error) {
	externalID = strings.TrimSpace(externalID)
	email = strings.ToLower(strings.TrimSpace(email))
	if externalID == "" {
		return nil, infraerrors.BadRequest("DEV2_EXTERNAL_ID_REQUIRED", "external_id is required")
	}
	if email == "" || len(email) > 255 {
		return nil, infraerrors.BadRequest("DEV2_EMAIL_INVALID", "invalid email")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, infraerrors.BadRequest("DEV2_EMAIL_INVALID", "invalid email")
	}
	client := h.IDEAuth.EntClient()
	if client == nil {
		return nil, service.ErrServiceUnavailable
	}

	ctx := c.Request.Context()
	identity, err := client.AuthIdentity.Query().
		Where(
			authidentity.ProviderTypeEQ(dev2ProviderType),
			authidentity.ProviderKeyEQ(dev2ProviderKey),
			authidentity.ProviderSubjectEQ(externalID),
		).
		Only(ctx)
	if err == nil {
		user, err := client.User.Get(ctx, identity.UserID)
		if err != nil {
			return nil, service.ErrUserNotFound
		}
		if !strings.EqualFold(strings.TrimSpace(user.Email), email) {
			return nil, service.ErrEmailExists
		}
		return entUserToService(user), nil
	}
	if !dbent.IsNotFound(err) {
		return nil, err
	}

	user, err := client.User.Query().Where(dbuser.EmailEqualFold(email)).Only(ctx)
	if err != nil && !dbent.IsNotFound(err) {
		return nil, err
	}
	if dbent.IsNotFound(err) {
		user, err = createDev2User(ctx, client, cfg, email)
		if err != nil {
			return nil, err
		}
	}
	if user == nil || user.ID <= 0 {
		return nil, service.ErrServiceUnavailable
	}

	if err := client.AuthIdentity.Create().
		SetUserID(user.ID).
		SetProviderType(dev2ProviderType).
		SetProviderKey(dev2ProviderKey).
		SetProviderSubject(externalID).
		SetVerifiedAt(time.Now().UTC()).
		SetMetadata(map[string]any{
			"email":  email,
			"source": "dev2_internal_sync",
		}).
		OnConflictColumns(
			authidentity.FieldProviderType,
			authidentity.FieldProviderKey,
			authidentity.FieldProviderSubject,
		).
		DoNothing().
		Exec(ctx); err != nil {
		return nil, err
	}
	return entUserToService(user), nil
}

func createDev2User(ctx context.Context, client *dbent.Client, cfg *config.Config, email string) (*dbent.User, error) {
	passwordHash, err := randomPasswordHash()
	if err != nil {
		return nil, err
	}
	balance := 0.0
	concurrency := 5
	if cfg != nil {
		balance = cfg.Default.UserBalance
		if cfg.Default.UserConcurrency > 0 {
			concurrency = cfg.Default.UserConcurrency
		}
	}
	return client.User.Create().
		SetEmail(email).
		SetPasswordHash(passwordHash).
		SetRole(service.RoleUser).
		SetBalance(balance).
		SetConcurrency(concurrency).
		SetStatus(service.StatusActive).
		SetSignupSource("email").
		Save(ctx)
}

func randomPasswordHash() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(buffer)), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func entUserToService(user *dbent.User) *service.User {
	if user == nil {
		return nil
	}
	return &service.User{
		ID:           user.ID,
		Email:        user.Email,
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		Role:         user.Role,
		Balance:      user.Balance,
		Concurrency:  user.Concurrency,
		Status:       user.Status,
		SignupSource: user.SignupSource,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
	}
}

func dev2UserPayload(user *service.User, externalID string) dev2UserResponse {
	if user == nil {
		return dev2UserResponse{}
	}
	return dev2UserResponse{
		ID:         user.ID,
		Email:      user.Email,
		ExternalID: strings.TrimSpace(externalID),
		Balance:    user.Balance,
		Status:     user.Status,
	}
}

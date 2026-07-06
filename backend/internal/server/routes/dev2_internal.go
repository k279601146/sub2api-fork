package routes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/mail"
	"os"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	dbapikey "github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/ent/redeemcode"
	"github.com/Wei-Shaw/sub2api/ent/usagelog"
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
	dev2ProviderType     = "dev2"
	dev2ProviderKey      = "dev2"
	dev2UsageAccountName = "__dev2_internal_billing__"
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

type dev2RewardGrantRequest struct {
	ExternalID string         `json:"external_id"`
	Email      string         `json:"email"`
	Credits    float64        `json:"credits"`
	Units      float64        `json:"units"`
	Reason     string         `json:"reason"`
	Reference  string         `json:"reference"`
	Metadata   map[string]any `json:"metadata"`
}

type dev2UsageRecordRequest struct {
	ExternalID   string         `json:"external_id"`
	Email        string         `json:"email"`
	RequestID    string         `json:"request_id"`
	ThreadID     string         `json:"thread_id"`
	Units        float64        `json:"units"`
	Model        string         `json:"model"`
	InputTokens  int            `json:"input_tokens"`
	OutputTokens int            `json:"output_tokens"`
	ToolNames    []string       `json:"tool_names"`
	Category     string         `json:"category"`
	Metadata     map[string]any `json:"metadata"`
}

type dev2UsageRefundRequest struct {
	ExternalID     string  `json:"external_id"`
	Email          string  `json:"email"`
	ThreadID       string  `json:"thread_id"`
	TurnID         string  `json:"turn_id"`
	Units          float64 `json:"units"`
	Reason         string  `json:"reason"`
	UsageRecordIDs []int64 `json:"usage_record_ids"`
	UsageRecords   []struct {
		ID         int64   `json:"id"`
		Units      float64 `json:"units"`
		ActualCost float64 `json:"actual_cost"`
	} `json:"usage_records"`
}

type dev2UsageRecordResponse struct {
	ID         int64                          `json:"id"`
	RequestID  string                         `json:"request_id"`
	Balance    float64                        `json:"balance"`
	Units      float64                        `json:"units"`
	ActualCost float64                        `json:"actual_cost"`
	Usage      *usagestats.UserDashboardStats `json:"usage,omitempty"`
}

type dev2UsageRefundResponse struct {
	ID         int64                          `json:"id"`
	RequestID  string                         `json:"request_id"`
	Balance    float64                        `json:"balance"`
	Units      float64                        `json:"units"`
	Credited   float64                        `json:"credited"`
	Usage      *usagestats.UserDashboardStats `json:"usage,omitempty"`
	Duplicated bool                           `json:"duplicated"`
}

type dev2RewardGrantResponse struct {
	ID         int64   `json:"id"`
	Reference  string  `json:"reference"`
	Balance    float64 `json:"balance"`
	Credits    float64 `json:"credits"`
	Units      float64 `json:"units"`
	Duplicated bool    `json:"duplicated"`
}

type dev2UsageAttribution struct {
	accountID int64
	apiKeyID  int64
}

type dev2ConfigResponse struct {
	ModelName string            `json:"model_name"`
	Env       map[string]string `json:"env,omitempty"`
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
		group.GET("/config", dev2Config(h, cfg))
		group.POST("/users/sync", dev2SyncUser(h, cfg))
		group.POST("/ide/auth/authorize", dev2AuthorizeIDE(h, cfg))
		group.GET("/billing/usage", dev2BillingUsage(h, cfg))
		group.POST("/billing/usage/check", dev2BillingUsage(h, cfg))
		group.POST("/billing/usage/record", dev2BillingUsageRecord(h, cfg))
		group.POST("/billing/usage/refund", dev2BillingUsageRefund(h, cfg))
		group.POST("/billing/rewards/grant", dev2BillingRewardGrant(h, cfg))
	}
}

func dev2Config(h *handler.Handlers, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		modelName := dev2ModelName(c, h, cfg)
		response.Success(c, dev2ConfigResponse{
			ModelName: modelName,
			Env:       dev2EnvConfig(c, h, modelName),
		})
	}
}

func dev2EnvConfig(c *gin.Context, h *handler.Handlers, modelName string) map[string]string {
	env := make(map[string]string)
	if h != nil && h.Admin != nil && h.Admin.Setting != nil {
		settings, err := h.Admin.Setting.GetAllSettings(c.Request.Context())
		if err == nil && settings != nil {
			for key, value := range parseDev2EnvConfig(settings.Dev2EnvConfig) {
				env[key] = value
			}
		}
	}
	if strings.TrimSpace(modelName) != "" {
		env["DEV2_DEFAULT_MODEL_ID"] = strings.TrimSpace(modelName)
	}
	return env
}

func parseDev2EnvConfig(raw string) map[string]string {
	env := make(map[string]string)
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		if key != "" {
			env[key] = value
		}
	}
	return env
}

func dev2ModelName(c *gin.Context, h *handler.Handlers, cfg *config.Config) string {
	// Priority 1: DB-backed setting (admin UI, no restart needed)
	if h != nil && h.Admin != nil && h.Admin.Setting != nil {
		settings, err := h.Admin.Setting.GetAllSettings(c.Request.Context())
		if err == nil && settings != nil && strings.TrimSpace(settings.Dev2ModelName) != "" {
			return strings.TrimSpace(settings.Dev2ModelName)
		}
	}
	// Priority 2: Environment variable
	modelName := strings.TrimSpace(os.Getenv("DEV2_MODEL_NAME"))
	if modelName != "" {
		return modelName
	}
	// Priority 3: config.yaml
	if cfg != nil {
		if configured := strings.TrimSpace(cfg.Dev2.ModelName); configured != "" {
			return configured
		}
	}
	// Priority 4: Viper fallback
	return strings.TrimSpace(viper.GetString("dev2.model_name"))
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

func dev2BillingUsageRecord(h *handler.Handlers, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req dev2UsageRecordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "invalid request")
			return
		}
		req.RequestID = strings.TrimSpace(req.RequestID)
		req.Model = strings.TrimSpace(req.Model)
		if req.RequestID == "" || len(req.RequestID) > 64 {
			response.BadRequest(c, "request_id is required and must be at most 64 characters")
			return
		}
		if req.Units <= 0 {
			response.BadRequest(c, "units must be greater than 0")
			return
		}

		user, err := upsertDev2User(c, h, cfg, req.ExternalID, req.Email)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if h.Usage == nil || h.Usage.UsageService() == nil {
			response.InternalError(c, "usage service is unavailable")
			return
		}
		attribution, err := ensureDev2UsageAttribution(c.Request.Context(), h, user.ID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}

		model := req.Model
		if model == "" {
			model = "dev2-agent"
		}
		log, err := h.Usage.UsageService().Create(c.Request.Context(), service.CreateUsageLogRequest{
			UserID:         user.ID,
			APIKeyID:       attribution.apiKeyID,
			AccountID:      attribution.accountID,
			RequestID:      req.RequestID,
			Model:          model,
			InputTokens:    req.InputTokens,
			OutputTokens:   req.OutputTokens,
			TotalCost:      req.Units,
			ActualCost:     req.Units,
			RateMultiplier: 1,
			BillingMode:    service.UsageBillingModeDev2Units,
			Stream:         true,
		})
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}

		client := h.IDEAuth.EntClient()
		if client == nil {
			response.ErrorFrom(c, service.ErrServiceUnavailable)
			return
		}
		updated, err := client.User.Get(c.Request.Context(), user.ID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		stats, err := h.Usage.UsageService().GetUserDashboardStats(c.Request.Context(), user.ID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}

		response.Success(c, dev2UsageRecordResponse{
			ID:         log.ID,
			RequestID:  req.RequestID,
			Balance:    updated.Balance,
			Units:      req.Units,
			ActualCost: log.ActualCost,
			Usage:      stats,
		})
	}
}

func dev2RefundUsageRecordIDs(req dev2UsageRefundRequest) []int64 {
	usageRecordIDs := make([]int64, 0, len(req.UsageRecordIDs)+len(req.UsageRecords))
	seenUsageRecordIDs := make(map[int64]struct{}, len(req.UsageRecordIDs)+len(req.UsageRecords))
	for _, id := range req.UsageRecordIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seenUsageRecordIDs[id]; ok {
			continue
		}
		seenUsageRecordIDs[id] = struct{}{}
		usageRecordIDs = append(usageRecordIDs, id)
	}
	for _, item := range req.UsageRecords {
		if item.ID <= 0 {
			continue
		}
		if _, ok := seenUsageRecordIDs[item.ID]; ok {
			continue
		}
		seenUsageRecordIDs[item.ID] = struct{}{}
		usageRecordIDs = append(usageRecordIDs, item.ID)
	}
	return usageRecordIDs
}

func dev2RefundAmountsFromOriginals(originals []*dbent.UsageLog) (float64, float64) {
	units := 0.0
	credit := 0.0
	for _, item := range originals {
		if item.TotalCost > 0 {
			units += item.TotalCost
		}
		if item.ActualCost > 0 {
			credit += item.ActualCost
		}
	}
	return units, credit
}

func dev2BillingUsageRefund(h *handler.Handlers, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req dev2UsageRefundRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "invalid request")
			return
		}
		req.ThreadID = strings.TrimSpace(req.ThreadID)
		req.TurnID = strings.TrimSpace(req.TurnID)
		req.Reason = strings.TrimSpace(req.Reason)
		if req.ThreadID == "" || req.TurnID == "" {
			response.BadRequest(c, "thread_id and turn_id are required")
			return
		}

		user, err := upsertDev2User(c, h, cfg, req.ExternalID, req.Email)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if h.Usage == nil || h.Usage.UsageService() == nil {
			response.InternalError(c, "usage service is unavailable")
			return
		}
		attribution, err := ensureDev2UsageAttribution(c.Request.Context(), h, user.ID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		client := h.IDEAuth.EntClient()
		if client == nil {
			response.ErrorFrom(c, service.ErrServiceUnavailable)
			return
		}

		requestID := dev2RefundRequestID(req.ThreadID, req.TurnID)
		existing, err := client.UsageLog.Query().
			Where(usagelog.RequestIDEQ(requestID), usagelog.UserIDEQ(user.ID)).
			First(c.Request.Context())
		if err == nil {
			updated, stats, snapshotErr := dev2UsageSnapshotForUser(c.Request.Context(), h, user.ID)
			if snapshotErr != nil {
				response.ErrorFrom(c, snapshotErr)
				return
			}
			response.Success(c, dev2UsageRefundResponse{
				ID:         existing.ID,
				RequestID:  requestID,
				Balance:    updated.Balance,
				Units:      math.Abs(existing.TotalCost),
				Credited:   math.Abs(existing.ActualCost),
				Usage:      stats,
				Duplicated: true,
			})
			return
		}
		if !dbent.IsNotFound(err) {
			response.ErrorFrom(c, err)
			return
		}

		units := math.Max(req.Units, 0)
		credit := 0.0
		usageRecordIDs := dev2RefundUsageRecordIDs(req)
		if len(usageRecordIDs) > 0 {
			originals, err := client.UsageLog.Query().
				Where(
					usagelog.IDIn(usageRecordIDs...),
					usagelog.UserIDEQ(user.ID),
					usagelog.BillingModeEQ(service.UsageBillingModeDev2Units),
				).
				All(c.Request.Context())
			if err != nil {
				response.ErrorFrom(c, err)
				return
			}
			units, credit = dev2RefundAmountsFromOriginals(originals)
		}
		units = math.Round(units*100) / 100
		credit = math.Round(credit*100) / 100
		if units <= 0 {
			response.BadRequest(c, "refundable units must be greater than 0")
			return
		}

		log, err := h.Usage.UsageService().Create(c.Request.Context(), service.CreateUsageLogRequest{
			UserID:         user.ID,
			APIKeyID:       attribution.apiKeyID,
			AccountID:      attribution.accountID,
			RequestID:      requestID,
			Model:          "dev2-refund",
			TotalCost:      -units,
			ActualCost:     -credit,
			RateMultiplier: 1,
			BillingMode:    service.UsageBillingModeDev2Units,
			Stream:         true,
		})
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if credit > 0 {
			if err := client.User.UpdateOneID(user.ID).AddBalance(credit).Exec(c.Request.Context()); err != nil {
				response.ErrorFrom(c, err)
				return
			}
		}
		updated, stats, err := dev2UsageSnapshotForUser(c.Request.Context(), h, user.ID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		response.Success(c, dev2UsageRefundResponse{
			ID:        log.ID,
			RequestID: requestID,
			Balance:   updated.Balance,
			Units:     units,
			Credited:  credit,
			Usage:     stats,
		})
	}
}

func dev2BillingRewardGrant(h *handler.Handlers, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req dev2RewardGrantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "invalid request")
			return
		}
		req.Reference = strings.TrimSpace(req.Reference)
		req.Reason = strings.TrimSpace(req.Reason)
		credits := req.Credits
		if credits <= 0 {
			credits = req.Units
		}
		if credits <= 0 {
			response.BadRequest(c, "credits must be greater than 0")
			return
		}
		if req.Reference == "" || len(req.Reference) > 32 {
			response.BadRequest(c, "reference is required and must be at most 32 characters")
			return
		}

		user, err := upsertDev2User(c, h, cfg, req.ExternalID, req.Email)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}

		client := h.IDEAuth.EntClient()
		if client == nil {
			response.ErrorFrom(c, service.ErrServiceUnavailable)
			return
		}

		existing, err := client.RedeemCode.Query().
			Where(redeemcode.CodeEQ(req.Reference)).
			Only(c.Request.Context())
		if err == nil {
			if existing.UsedBy != nil && *existing.UsedBy == user.ID {
				response.Success(c, dev2RewardGrantResponse{
					ID:         existing.ID,
					Reference:  req.Reference,
					Balance:    user.Balance,
					Credits:    existing.Value,
					Units:      existing.Value,
					Duplicated: true,
				})
				return
			}
			response.BadRequest(c, "reward reference already exists")
			return
		}
		if !dbent.IsNotFound(err) {
			response.ErrorFrom(c, err)
			return
		}

		notesPayload := map[string]any{
			"source":   "dev2_referral_reward",
			"reason":   req.Reason,
			"metadata": req.Metadata,
		}
		tx, err := client.Tx(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		defer func() { _ = tx.Rollback() }()

		notesBytes, _ := json.Marshal(notesPayload)
		now := time.Now().UTC()
		created, err := tx.Client().RedeemCode.Create().
			SetCode(req.Reference).
			SetType(service.RedeemTypeBalance).
			SetValue(credits).
			SetStatus(service.StatusUsed).
			SetUsedBy(user.ID).
			SetUsedAt(now).
			SetNotes(string(notesBytes)).
			Save(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if err := tx.Client().User.UpdateOneID(user.ID).AddBalance(credits).AddTotalRecharged(credits).Exec(c.Request.Context()); err != nil {
			response.ErrorFrom(c, err)
			return
		}
		updated, err := tx.Client().User.Get(c.Request.Context(), user.ID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if err := tx.Commit(); err != nil {
			response.ErrorFrom(c, err)
			return
		}

		response.Success(c, dev2RewardGrantResponse{
			ID:        created.ID,
			Reference: req.Reference,
			Balance:   updated.Balance,
			Credits:   credits,
			Units:     credits,
		})
	}
}

func ensureDev2UsageAttribution(ctx context.Context, h *handler.Handlers, userID int64) (*dev2UsageAttribution, error) {
	if h == nil || h.IDEAuth == nil {
		return nil, service.ErrServiceUnavailable
	}
	client := h.IDEAuth.EntClient()
	if client == nil {
		return nil, service.ErrServiceUnavailable
	}

	account, err := client.Account.Query().
		Where(
			dbaccount.NameEQ(dev2UsageAccountName),
			dbaccount.PlatformEQ(dev2ProviderType),
			dbaccount.TypeEQ(service.AccountTypeAPIKey),
		).
		First(ctx)
	if err != nil {
		if !dbent.IsNotFound(err) {
			return nil, err
		}
		account, err = client.Account.Create().
			SetName(dev2UsageAccountName).
			SetPlatform(dev2ProviderType).
			SetType(service.AccountTypeAPIKey).
			SetCredentials(map[string]any{"source": "dev2_internal_billing"}).
			SetExtra(map[string]any{"hidden": true, "source": "dev2_internal_billing"}).
			SetConcurrency(1).
			SetPriority(9999).
			SetStatus(service.StatusDisabled).
			SetSchedulable(false).
			Save(ctx)
		if err != nil {
			return nil, err
		}
	}

	keyValue := fmt.Sprintf("dev2-internal-user-%d", userID)
	apiKey, err := client.APIKey.Query().
		Where(dbapikey.KeyEQ(keyValue)).
		First(ctx)
	if err != nil {
		if !dbent.IsNotFound(err) {
			return nil, err
		}
		apiKey, err = client.APIKey.Create().
			SetUserID(userID).
			SetKey(keyValue).
			SetName("Dev2 internal billing").
			SetStatus(service.StatusDisabled).
			Save(ctx)
		if err != nil {
			existing, findErr := client.APIKey.Query().Where(dbapikey.KeyEQ(keyValue)).First(ctx)
			if findErr == nil {
				apiKey = existing
			} else {
				return nil, err
			}
		}
	}

	if apiKey.UserID != userID {
		return nil, infraerrors.InternalServer("DEV2_USAGE_API_KEY_OWNER_MISMATCH", "dev2 usage attribution key owner mismatch")
	}
	return &dev2UsageAttribution{accountID: account.ID, apiKeyID: apiKey.ID}, nil
}

func dev2RefundRequestID(threadID, turnID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(threadID) + ":" + strings.TrimSpace(turnID) + ":refund"))
	return "dev2refund:" + hex.EncodeToString(sum[:])[:32]
}

func dev2UsageSnapshotForUser(ctx context.Context, h *handler.Handlers, userID int64) (*dbent.User, *usagestats.UserDashboardStats, error) {
	if h == nil || h.IDEAuth == nil || h.Usage == nil || h.Usage.UsageService() == nil {
		return nil, nil, service.ErrServiceUnavailable
	}
	client := h.IDEAuth.EntClient()
	if client == nil {
		return nil, nil, service.ErrServiceUnavailable
	}
	updated, err := client.User.Get(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	stats, err := h.Usage.UsageService().GetUserDashboardStats(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return updated, stats, nil
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

package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type modelRegionAccountRepoStub struct {
	service.AccountRepository
	accounts []service.Account
}

func (s modelRegionAccountRepoStub) ListSchedulableByGroupID(context.Context, int64) ([]service.Account, error) {
	return s.accounts, nil
}

func TestGatewayHandlerModels_FiltersCNAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(9)
	repo := modelRegionAccountRepoStub{
		accounts: []service.Account{
			{
				ID:       1,
				Platform: service.PlatformAnthropic,
				Credentials: map[string]any{
					"model_mapping": map[string]any{
						"gpt-5.4":           "gpt-5.4",
						"gpt-5.5":           "gpt-5.5",
						"claude-3-5-sonnet": "claude-3-5-sonnet",
					},
				},
			},
		},
	}
	gatewayService := service.NewGatewayService(
		repo, nil, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	regionPolicy := service.NewModelRegionPolicy(&config.Config{
		ModelRegionIsolation: config.ModelRegionIsolationConfig{
			Enabled:         true,
			CNAllowedModels: []string{"gpt-5.4", "claude-3-5-sonnet"},
		},
	})
	handler := &GatewayHandler{
		gatewayService:    gatewayService,
		modelRegionPolicy: regionPolicy,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID:      1,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformAnthropic},
	})

	handler.Models(c)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "list", gjson.GetBytes(w.Body.Bytes(), "object").String())
	models := gjson.GetBytes(w.Body.Bytes(), "data").Array()
	require.Len(t, models, 2)
	require.Equal(t, "claude-3-5-sonnet", models[0].Get("id").String())
	require.Equal(t, "gpt-5.4", models[1].Get("id").String())
}

func TestGatewayHandlerMessages_RejectsCNDisallowedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	regionPolicy := service.NewModelRegionPolicy(&config.Config{
		ModelRegionIsolation: config.ModelRegionIsolationConfig{
			Enabled:         true,
			CNAllowedModels: []string{"claude-3-5-sonnet"},
		},
	})
	handler := &GatewayHandler{
		gatewayService:    &service.GatewayService{},
		modelRegionPolicy: regionPolicy,
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString(`{"model":"gpt-5.5"}`))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})

	handler.Messages(c)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, modelRegionDeniedMessage, gjson.GetBytes(w.Body.Bytes(), "error.message").String())
}

func TestGatewayHandlerGeminiV1BetaModels_RejectsCNDisallowedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	regionPolicy := service.NewModelRegionPolicy(&config.Config{
		ModelRegionIsolation: config.ModelRegionIsolationConfig{
			Enabled:         true,
			CNAllowedModels: []string{"models/gemini-2.5-flash"},
		},
	})
	handler := &GatewayHandler{
		modelRegionPolicy: regionPolicy,
	}

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3-pro-preview:generateContent", bytes.NewBufferString(`{"contents":[]}`))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-3-pro-preview:generateContent"}}
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
		ID:    1,
		Group: &service.Group{Platform: service.PlatformGemini},
	})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})

	handler.GeminiV1BetaModels(c)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, modelRegionDeniedMessage, gjson.GetBytes(w.Body.Bytes(), "error.message").String())
}

func TestOpenAIGatewayHandlerChatCompletions_RejectsCNDisallowedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	regionPolicy := service.NewModelRegionPolicy(&config.Config{
		ModelRegionIsolation: config.ModelRegionIsolationConfig{
			Enabled:         true,
			CNAllowedModels: []string{"claude-3-5-sonnet"},
		},
	})
	handler := &OpenAIGatewayHandler{
		gatewayService:           &service.OpenAIGatewayService{},
		billingCacheService:      &service.BillingCacheService{},
		apiKeyService:            &service.APIKeyService{},
		concurrencyHelper:        NewConcurrencyHelper(&service.ConcurrencyService{}, SSEPingFormatComment, 0),
		usageRecordWorkerPool:    nil,
		errorPassthroughService:  nil,
		contentModerationService: nil,
		modelRegionPolicy:        regionPolicy,
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"gpt-5.5"}`))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1, Group: &service.Group{Platform: service.PlatformAnthropic}})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1, Concurrency: 1})

	handler.ChatCompletions(c)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, modelRegionDeniedMessage, gjson.GetBytes(w.Body.Bytes(), "error.message").String())
}

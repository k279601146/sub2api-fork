package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const modelRegionDeniedMessage = "model is not available in your region"

type modelRegionScopeContextKey struct{}

func (h *GatewayHandler) modelRegionScope(c *gin.Context) string {
	if h == nil || h.modelRegionPolicy == nil {
		return service.ModelRegionScopeGlobal
	}
	return resolveModelRegionScope(c, h.modelRegionPolicy)
}

func (h *OpenAIGatewayHandler) modelRegionScope(c *gin.Context) string {
	if h == nil || h.modelRegionPolicy == nil {
		return service.ModelRegionScopeGlobal
	}
	return resolveModelRegionScope(c, h.modelRegionPolicy)
}

func resolveModelRegionScope(c *gin.Context, policy *service.ModelRegionPolicy) string {
	if c == nil || policy == nil {
		return service.ModelRegionScopeGlobal
	}
	if scope, ok := c.Request.Context().Value(modelRegionScopeContextKey{}).(string); ok && scope != "" {
		return scope
	}
	scope := policy.ScopeForClientIP(ip.GetTrustedClientIP(c))
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), modelRegionScopeContextKey{}, scope))
	return scope
}

func (h *GatewayHandler) ensureModelAllowedForRegion(c *gin.Context, model string) bool {
	if h == nil || h.modelRegionPolicy == nil {
		return true
	}
	if h.modelRegionPolicy.IsModelAllowed(h.modelRegionScope(c), model) {
		return true
	}
	h.errorResponse(c, http.StatusForbidden, "permission_error", modelRegionDeniedMessage)
	return false
}

func (h *GatewayHandler) ensureChatModelAllowedForRegion(c *gin.Context, model string) bool {
	if h == nil || h.modelRegionPolicy == nil {
		return true
	}
	if h.modelRegionPolicy.IsModelAllowed(h.modelRegionScope(c), model) {
		return true
	}
	h.chatCompletionsErrorResponse(c, http.StatusForbidden, "permission_error", modelRegionDeniedMessage)
	return false
}

func (h *GatewayHandler) ensureResponsesModelAllowedForRegion(c *gin.Context, model string) bool {
	if h == nil || h.modelRegionPolicy == nil {
		return true
	}
	if h.modelRegionPolicy.IsModelAllowed(h.modelRegionScope(c), model) {
		return true
	}
	h.responsesErrorResponse(c, http.StatusForbidden, "permission_error", modelRegionDeniedMessage)
	return false
}

func (h *GatewayHandler) ensureGeminiModelAllowedForRegion(c *gin.Context, model string) bool {
	if h == nil || h.modelRegionPolicy == nil {
		return true
	}
	if h.modelRegionPolicy.IsModelAllowed(h.modelRegionScope(c), model) {
		return true
	}
	googleError(c, http.StatusForbidden, modelRegionDeniedMessage)
	return false
}

func (h *OpenAIGatewayHandler) ensureModelAllowedForRegion(c *gin.Context, model string) bool {
	if h == nil || h.modelRegionPolicy == nil {
		return true
	}
	if h.modelRegionPolicy.IsModelAllowed(h.modelRegionScope(c), model) {
		return true
	}
	h.errorResponse(c, http.StatusForbidden, "permission_error", modelRegionDeniedMessage)
	return false
}

func (h *GatewayHandler) filterRegionModelIDs(c *gin.Context, models []string) []string {
	if h == nil || h.modelRegionPolicy == nil {
		return models
	}
	return h.modelRegionPolicy.FilterModels(h.modelRegionScope(c), models)
}

func filterRegionModels[T any](policy *service.ModelRegionPolicy, scope string, models []T, modelID func(T) string) []T {
	if policy == nil || scope != service.ModelRegionScopeCN {
		return models
	}
	filtered := make([]T, 0, len(models))
	for _, model := range models {
		if policy.IsModelAllowed(scope, modelID(model)) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func (h *GatewayHandler) isRegionFiltered(c *gin.Context) bool {
	return h != nil && h.modelRegionPolicy != nil && h.modelRegionScope(c) == service.ModelRegionScopeCN
}

func (h *GatewayHandler) filterGeminiModelsPayload(c *gin.Context, body []byte) ([]byte, bool) {
	if !h.isRegionFiltered(c) || !gjson.ValidBytes(body) {
		return body, false
	}
	models := gjson.GetBytes(body, "models")
	if !models.IsArray() {
		return body, false
	}
	filtered := make([]any, 0, len(models.Array()))
	for _, item := range models.Array() {
		name := strings.TrimSpace(item.Get("name").String())
		if h.modelRegionPolicy.IsModelAllowed(service.ModelRegionScopeCN, name) {
			filtered = append(filtered, item.Value())
		}
	}
	next, err := sjson.SetBytes(body, "models", filtered)
	if err != nil {
		return body, false
	}
	return next, true
}

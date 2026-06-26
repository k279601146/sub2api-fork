package middleware

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Logger 请求日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 开始时间
		startTime := time.Now()

		// 请求路径
		path := c.Request.URL.Path

		// 处理请求
		c.Next()

		// 跳过健康检查等高频探针路径的日志
		if path == "/health" || path == "/setup/status" {
			return
		}

		endTime := time.Now()
		latency := endTime.Sub(startTime)

		method := c.Request.Method
		statusCode := c.Writer.Status()
		clientIP := ip.GetClientIP(c)
		protocol := c.Request.Proto
		accountID, hasAccountID := c.Request.Context().Value(ctxkey.AccountID).(int64)
		platform, _ := c.Request.Context().Value(ctxkey.Platform).(string)
		model, _ := c.Request.Context().Value(ctxkey.Model).(string)

		fields := []zap.Field{
			zap.String("component", "http.access"),
			zap.Int("status_code", statusCode),
			zap.Int64("latency_ms", latency.Milliseconds()),
			zap.String("client_ip", clientIP),
			zap.String("protocol", protocol),
			zap.String("method", method),
			zap.String("path", path),
		}
		if hasAccountID && accountID > 0 {
			fields = append(fields, zap.Int64("account_id", accountID))
		}
		if platform != "" {
			fields = append(fields, zap.String("platform", platform))
		}
		if model != "" {
			fields = append(fields, zap.String("model", model))
		}
		fields = appendLatencyField(c, fields, service.OpsAuthLatencyMsKey, "auth_latency_ms")
		fields = appendLatencyField(c, fields, service.OpsRoutingLatencyMsKey, "routing_latency_ms")
		fields = appendLatencyField(c, fields, service.OpsUpstreamLatencyMsKey, "upstream_latency_ms")
		fields = appendLatencyField(c, fields, service.OpsResponseLatencyMsKey, "response_latency_ms")
		fields = appendLatencyField(c, fields, service.OpsTimeToFirstTokenMsKey, "time_to_first_token_ms")

		l := logger.FromContext(c.Request.Context()).Named("http.access").With(fields...)
		l.Info("http request completed", zap.Time("completed_at", endTime))

		if len(c.Errors) > 0 {
			l.Warn("http request contains gin errors", zap.String("errors", c.Errors.String()))
		}
	}
}

func appendLatencyField(c *gin.Context, fields []zap.Field, contextKey string, fieldName string) []zap.Field {
	value, exists := c.Get(contextKey)
	if !exists {
		return fields
	}
	switch typed := value.(type) {
	case int64:
		return append(fields, zap.Int64(fieldName, typed))
	case int:
		return append(fields, zap.Int(fieldName, typed))
	}
	return fields
}

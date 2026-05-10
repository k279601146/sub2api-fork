package routes

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/middleware"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RegisterIDERoutes registers desktop IDE integration endpoints at /ide/*.
func RegisterIDERoutes(
	r *gin.Engine,
	h *handler.Handlers,
	jwtAuth servermiddleware.JWTAuthMiddleware,
	redisClient *redis.Client,
	settingService *service.SettingService,
) {
	rateLimiter := middleware.NewRateLimiter(redisClient)

	auth := r.Group("/ide/auth")
	auth.Use(servermiddleware.BackendModeAuthGuard(settingService))
	{
		auth.POST("/token", rateLimiter.LimitWithOptions("ide-auth-token", 30, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.IDEAuth.Token)
	}

	authenticated := r.Group("/ide")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(servermiddleware.BackendModeUserGuard(settingService))
	{
		authenticated.GET("/auth/me", h.IDEAuth.Me)
		authenticated.POST("/auth/revoke", h.IDEAuth.Revoke)
	}
}

package routes

import (
	"net/http"
	"os"
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

		api := authenticated.Group("/api")
		{
			usage := api.Group("/usage")
			{
				usage.GET("", h.Usage.DashboardStats)
				usage.GET("/stats", h.Usage.Stats)
				usage.GET("/trend", h.Usage.DashboardTrend)
				usage.GET("/models", h.Usage.DashboardModels)
			}

			plan := api.Group("/plan")
			{
				plan.GET("", h.Subscription.GetSummary)
				plan.GET("/progress", h.Subscription.GetProgress)
			}

			if h.Gateway != nil {
				api.GET("/models", h.Gateway.Models)
			}

			version := api.Group("/version")
			{
				version.GET("/app", ideVersionResponse("app"))
				version.GET("/engine", ideVersionResponse("engine"))
			}
		}
	}
}

func ideVersionResponse(kind string) gin.HandlerFunc {
	prefix := "IDE_APP"
	if kind == "engine" {
		prefix = "IDE_ENGINE"
	}
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "success",
			"data": gin.H{
				"kind":          kind,
				"version":       os.Getenv(prefix + "_LATEST_VERSION"),
				"download_url":  os.Getenv(prefix + "_DOWNLOAD_URL"),
				"manifest_url":  os.Getenv(prefix + "_MANIFEST_URL"),
				"sha256":        os.Getenv(prefix + "_SHA256"),
				"release_notes": os.Getenv(prefix + "_RELEASE_NOTES"),
			},
		})
	}
}

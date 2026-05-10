package routes

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	idehandler "github.com/Wei-Shaw/sub2api/internal/handler/ide"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIDERoutesExposeClientUsageAndPlanAPIs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	RegisterIDERoutes(
		router,
		&handler.Handlers{
			IDEAuth:      &idehandler.AuthHandler{},
			Usage:        &handler.UsageHandler{},
			Subscription: &handler.SubscriptionHandler{},
		},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.Next()
		}),
		nil,
		nil,
	)

	registered := map[string]string{}
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = route.Handler
	}

	for _, route := range []string{
		http.MethodGet + " /ide/api/usage",
		http.MethodGet + " /ide/api/usage/stats",
		http.MethodGet + " /ide/api/usage/trend",
		http.MethodGet + " /ide/api/usage/models",
		http.MethodGet + " /ide/api/plan",
		http.MethodGet + " /ide/api/plan/progress",
		http.MethodGet + " /ide/api/version/app",
		http.MethodGet + " /ide/api/version/engine",
	} {
		require.NotEmpty(t, registered[route], "missing route %s", route)
	}
}

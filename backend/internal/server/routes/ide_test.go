package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		http.MethodGet + " /ide/auth/authorize",
		http.MethodGet + " /ide/auth/callback",
		http.MethodGet + " /ide/api/usage",
		http.MethodGet + " /ide/api/usage/stats",
		http.MethodGet + " /ide/api/usage/trend",
		http.MethodGet + " /ide/api/usage/models",
		http.MethodGet + " /ide/api/plan",
		http.MethodGet + " /ide/api/plan/progress",
		http.MethodGet + " /ide/api/version/app",
		http.MethodGet + " /ide/api/version/engine",
		http.MethodPost + " /ide/api/telemetry",
	} {
		require.NotEmpty(t, registered[route], "missing route %s", route)
	}
}

func TestIDEAdminRoutesExposeSessionStatsAndReleaseManagement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	admin := router.Group("/api/v1/admin")

	RegisterIDEAdminRoutes(admin, &handler.Handlers{
		IDEAuth: &idehandler.AuthHandler{},
	})

	registered := map[string]string{}
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = route.Handler
	}

	for _, route := range []string{
		http.MethodGet + " /api/v1/admin/ide/sessions",
		http.MethodPost + " /api/v1/admin/ide/sessions/:id/revoke",
		http.MethodGet + " /api/v1/admin/ide/stats",
		http.MethodGet + " /api/v1/admin/ide/releases",
		http.MethodPost + " /api/v1/admin/ide/releases",
	} {
		require.NotEmpty(t, registered[route], "missing route %s", route)
	}
}

func TestIDEVersionResponseIncludesUpdaterFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ideReleaseMemory.Lock()
	ideReleaseMemory.releases = map[string]ideReleaseRecord{}
	ideReleaseMemory.Unlock()
	t.Setenv("IDE_ENGINE_LATEST_VERSION", "2.0.0")
	t.Setenv("IDE_ENGINE_WIN32_X64_DOWNLOAD_URL", "https://cdn.example.com/engine.exe")
	t.Setenv("IDE_ENGINE_WIN32_X64_SHA256", "abc123")
	t.Setenv("IDE_ENGINE_WIN32_X64_SIZE", "12345")
	t.Setenv("IDE_ENGINE_MIN_APP_VERSION", "1.4.0")
	t.Setenv("IDE_ENGINE_MANDATORY", "true")

	router := gin.New()
	router.GET("/ide/api/version/engine", ideVersionResponse("engine", nil))

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ide/api/version/engine?current=1.0.0&platform=win32&arch=x64", nil)
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"latest_version":"2.0.0"`)
	require.Contains(t, recorder.Body.String(), `"current_version":"1.0.0"`)
	require.Contains(t, recorder.Body.String(), `"has_update":true`)
	require.Contains(t, recorder.Body.String(), `"is_mandatory":true`)
	require.Contains(t, recorder.Body.String(), `"min_app_version":"1.4.0"`)
	require.Contains(t, recorder.Body.String(), `"url":"https://cdn.example.com/engine.exe"`)
	require.Contains(t, recorder.Body.String(), `"sha256":"abc123"`)
	require.Contains(t, recorder.Body.String(), `"size":12345`)
}

func TestIDEAdminPublishedReleaseFeedsVersionEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ideReleaseMemory.Lock()
	ideReleaseMemory.releases = map[string]ideReleaseRecord{}
	ideReleaseMemory.Unlock()

	router := gin.New()
	router.POST("/api/v1/admin/ide/releases", ideAdminPublishRelease(nil))
	router.GET("/ide/api/version/engine", ideVersionResponse("engine", nil))

	publishBody := bytes.NewBufferString(`{
		"kind":"engine",
		"version":"3.0.0",
		"min_app_version":"2.0.0",
		"is_mandatory":true,
		"binaries":{"win32-x64":{"url":"https://cdn.example.com/engine-3.exe","sha256":"def456","size":98765}}
	}`)
	publishRecorder := httptest.NewRecorder()
	publishReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ide/releases", publishBody)
	publishReq.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(publishRecorder, publishReq)

	require.Equal(t, http.StatusCreated, publishRecorder.Code)

	versionRecorder := httptest.NewRecorder()
	versionReq := httptest.NewRequest(http.MethodGet, "/ide/api/version/engine?current=2.9.0&platform=win32&arch=x64", nil)
	router.ServeHTTP(versionRecorder, versionReq)

	require.Equal(t, http.StatusOK, versionRecorder.Code)
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			LatestVersion string `json:"latest_version"`
			HasUpdate     bool   `json:"has_update"`
			DownloadURL   string `json:"download_url"`
			Download      struct {
				SHA256 string `json:"sha256"`
				Size   int64  `json:"size"`
			} `json:"download"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(versionRecorder.Body.Bytes(), &envelope))
	require.Equal(t, 0, envelope.Code)
	require.Equal(t, "3.0.0", envelope.Data.LatestVersion)
	require.True(t, envelope.Data.HasUpdate)
	require.Equal(t, "https://cdn.example.com/engine-3.exe", envelope.Data.DownloadURL)
	require.Equal(t, "def456", envelope.Data.Download.SHA256)
	require.Equal(t, int64(98765), envelope.Data.Download.Size)
}

func TestIDETelemetryResponseAcceptsBoundedBatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/ide/api/telemetry", ideTelemetryResponse())

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ide/api/telemetry", strings.NewReader(`{"events":[{"type":"ttft","timestamp":"2026-05-10T00:00:00Z","data":{"duration_ms":123}}]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
}

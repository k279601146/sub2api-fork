package routes

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/iderelease"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/middleware"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type ideReleaseBinary struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

type ideReleaseRecord struct {
	Kind          string                      `json:"kind"`
	Version       string                      `json:"version"`
	LatestVersion string                      `json:"latest_version"`
	MinAppVersion string                      `json:"min_app_version,omitempty"`
	Binaries      map[string]ideReleaseBinary `json:"binaries,omitempty"`
	ReleaseNotes  string                      `json:"release_notes,omitempty"`
	IsMandatory   bool                        `json:"is_mandatory"`
	PublishedAt   string                      `json:"published_at"`
}

var ideReleaseMemory = struct {
	sync.RWMutex
	releases map[string]ideReleaseRecord
}{
	releases: map[string]ideReleaseRecord{},
}

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
		auth.GET("/authorize", rateLimiter.LimitWithOptions("ide-auth-authorize", 60, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.IDEAuth.Authorize)
		auth.GET("/callback", rateLimiter.LimitWithOptions("ide-auth-callback", 60, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.IDEAuth.Callback)
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
				version.GET("/app", ideVersionResponse("app", h.IDEAuth.EntClient()))
				version.GET("/engine", ideVersionResponse("engine", h.IDEAuth.EntClient()))
			}
			api.POST("/telemetry", ideTelemetryResponse())
		}
	}
}

func RegisterIDEAdminRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	ide := admin.Group("/ide")
	{
		ide.GET("/sessions", h.IDEAuth.ListSessions)
		ide.POST("/sessions/:id/revoke", h.IDEAuth.RevokeSession)
		ide.GET("/stats", h.IDEAuth.Stats)
		ide.GET("/releases", ideAdminListReleases(h.IDEAuth.EntClient()))
		ide.POST("/releases", ideAdminPublishRelease(h.IDEAuth.EntClient()))
	}
}

func ideVersionResponse(kind string, entClient *dbent.Client) gin.HandlerFunc {
	prefix := "IDE_APP"
	if kind == "engine" {
		prefix = "IDE_ENGINE"
	}
	return func(c *gin.Context) {
		currentVersion := strings.TrimSpace(c.Query("current"))
		platform := strings.TrimSpace(c.Query("platform"))
		arch := strings.TrimSpace(c.Query("arch"))
		platformKey := platform
		if platformKey != "" && arch != "" && !strings.Contains(platformKey, "-") {
			platformKey += "-" + arch
		}

		if release, ok := getPublishedIDERelease(c.Request.Context(), entClient, kind); ok {
			binary := releaseBinaryForPlatform(release, platformKey)
			c.JSON(http.StatusOK, gin.H{
				"code":    0,
				"message": "success",
				"data": gin.H{
					"kind":            kind,
					"version":         release.Version,
					"latest_version":  release.LatestVersion,
					"current_version": currentVersion,
					"has_update":      release.LatestVersion != "" && currentVersion != "" && release.LatestVersion != currentVersion,
					"is_mandatory":    release.IsMandatory,
					"min_app_version": release.MinAppVersion,
					"published_at":    release.PublishedAt,
					"download_url":    binary.URL,
					"manifest_url":    "",
					"sha256":          binary.SHA256,
					"release_notes":   release.ReleaseNotes,
					"binaries":        release.Binaries,
					"download": gin.H{
						"url":    binary.URL,
						"sha256": binary.SHA256,
						"size":   nullableReleaseSize(binary.Size),
					},
				},
			})
			return
		}

		latestVersion := strings.TrimSpace(os.Getenv(prefix + "_LATEST_VERSION"))
		downloadURL := resolveIDEReleaseEnv(prefix, platformKey, "DOWNLOAD_URL")
		manifestURL := resolveIDEReleaseEnv(prefix, platformKey, "MANIFEST_URL")
		sha256 := resolveIDEReleaseEnv(prefix, platformKey, "SHA256")
		size := parseOptionalInt64(resolveIDEReleaseEnv(prefix, platformKey, "SIZE"))
		minAppVersion := strings.TrimSpace(os.Getenv(prefix + "_MIN_APP_VERSION"))
		mandatory := parseBoolEnv(prefix + "_MANDATORY")
		publishedAt := strings.TrimSpace(os.Getenv(prefix + "_PUBLISHED_AT"))
		releaseNotes := strings.TrimSpace(os.Getenv(prefix + "_RELEASE_NOTES"))

		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "success",
			"data": gin.H{
				"kind":            kind,
				"version":         latestVersion,
				"latest_version":  latestVersion,
				"current_version": currentVersion,
				"has_update":      latestVersion != "" && currentVersion != "" && latestVersion != currentVersion,
				"is_mandatory":    mandatory,
				"min_app_version": minAppVersion,
				"published_at":    publishedAt,
				"download_url":    downloadURL,
				"manifest_url":    manifestURL,
				"sha256":          sha256,
				"release_notes":   releaseNotes,
				"download": gin.H{
					"url":    downloadURL,
					"sha256": sha256,
					"size":   size,
				},
			},
		})
	}
}

func ideAdminListReleases(entClient *dbent.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		releases := []ideReleaseRecord{}
		for _, kind := range []string{"app", "engine"} {
			if release, ok := getPublishedIDERelease(c.Request.Context(), entClient, kind); ok {
				releases = append(releases, release)
				continue
			}
			release := releaseFromEnv(kind)
			if release.LatestVersion != "" || len(release.Binaries) > 0 {
				releases = append(releases, release)
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "success",
			"data": gin.H{
				"items": releases,
				"total": len(releases),
			},
		})
	}
}

func ideAdminPublishRelease(entClient *dbent.Client) gin.HandlerFunc {
	type publishReleaseRequest struct {
		Kind          string                      `json:"kind"`
		Type          string                      `json:"type"`
		Version       string                      `json:"version"`
		MinAppVersion string                      `json:"min_app_version"`
		Binaries      map[string]ideReleaseBinary `json:"binaries"`
		ReleaseNotes  string                      `json:"release_notes"`
		IsMandatory   bool                        `json:"is_mandatory"`
	}

	return func(c *gin.Context) {
		var req publishReleaseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid release payload"})
			return
		}
		kind := strings.TrimSpace(req.Kind)
		if kind == "" {
			kind = strings.TrimSpace(req.Type)
		}
		if kind != "app" && kind != "engine" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "kind must be app or engine"})
			return
		}
		version := strings.TrimSpace(req.Version)
		if version == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "version is required"})
			return
		}
		binaries := normalizeReleaseBinaries(req.Binaries)
		if len(binaries) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "at least one release binary is required"})
			return
		}

		release := ideReleaseRecord{
			Kind:          kind,
			Version:       version,
			LatestVersion: version,
			MinAppVersion: strings.TrimSpace(req.MinAppVersion),
			Binaries:      binaries,
			ReleaseNotes:  strings.TrimSpace(req.ReleaseNotes),
			IsMandatory:   req.IsMandatory,
			PublishedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		if entClient != nil {
			publishedAt := time.Now().UTC()
			if _, err := entClient.IDERelease.Update().
				Where(iderelease.Kind(kind), iderelease.IsLatest(true)).
				SetIsLatest(false).
				Save(c.Request.Context()); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to retire previous IDE releases"})
				return
			}
			row, err := entClient.IDERelease.Create().
				SetKind(kind).
				SetVersion(version).
				SetMinAppVersion(release.MinAppVersion).
				SetBinaries(releaseBinariesToJSON(binaries)).
				SetReleaseNotes(release.ReleaseNotes).
				SetIsMandatory(req.IsMandatory).
				SetIsLatest(true).
				SetPublishedAt(publishedAt).
				Save(c.Request.Context())
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to publish IDE release"})
				return
			}
			release = ideReleaseRecordFromEnt(row)
		} else {
			ideReleaseMemory.Lock()
			ideReleaseMemory.releases[kind] = release
			ideReleaseMemory.Unlock()
		}

		c.JSON(http.StatusCreated, gin.H{
			"code":    0,
			"message": "success",
			"data":    release,
		})
	}
}

func ideTelemetryResponse() gin.HandlerFunc {
	type telemetryEvent struct {
		Type      string         `json:"type"`
		Timestamp string         `json:"timestamp"`
		Data      map[string]any `json:"data"`
	}
	type telemetryRequest struct {
		Events []telemetryEvent `json:"events"`
	}

	return func(c *gin.Context) {
		var req telemetryRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid telemetry payload"})
			return
		}
		if len(req.Events) > 100 {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": 413, "message": "too many telemetry events"})
			return
		}

		c.Status(http.StatusNoContent)
	}
}

func resolveIDEReleaseEnv(prefix, platformKey, suffix string) string {
	if platformKey != "" {
		key := prefix + "_" + normalizeIDEReleasePlatformKey(platformKey) + "_" + suffix
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return strings.TrimSpace(os.Getenv(prefix + "_" + suffix))
}

func getPublishedIDERelease(ctx context.Context, entClient *dbent.Client, kind string) (ideReleaseRecord, bool) {
	if entClient != nil {
		row, err := entClient.IDERelease.Query().
			Where(iderelease.Kind(kind), iderelease.IsLatest(true)).
			Order(dbent.Desc(iderelease.FieldPublishedAt)).
			First(ctx)
		if err == nil {
			return ideReleaseRecordFromEnt(row), true
		}
	}

	ideReleaseMemory.RLock()
	release, ok := ideReleaseMemory.releases[kind]
	ideReleaseMemory.RUnlock()
	return release, ok
}

func releaseFromEnv(kind string) ideReleaseRecord {
	prefix := "IDE_APP"
	if kind == "engine" {
		prefix = "IDE_ENGINE"
	}
	binaries := map[string]ideReleaseBinary{}
	for _, platformKey := range []string{"win32-x64", "darwin-x64", "darwin-arm64", "linux-x64"} {
		binary := ideReleaseBinary{
			URL:    resolveIDEReleaseEnv(prefix, platformKey, "DOWNLOAD_URL"),
			SHA256: resolveIDEReleaseEnv(prefix, platformKey, "SHA256"),
			Size:   parseInt64OrZero(resolveIDEReleaseEnv(prefix, platformKey, "SIZE")),
		}
		if binary.URL != "" || binary.SHA256 != "" || binary.Size > 0 {
			binaries[platformKey] = binary
		}
	}
	latestVersion := strings.TrimSpace(os.Getenv(prefix + "_LATEST_VERSION"))
	return ideReleaseRecord{
		Kind:          kind,
		Version:       latestVersion,
		LatestVersion: latestVersion,
		MinAppVersion: strings.TrimSpace(os.Getenv(prefix + "_MIN_APP_VERSION")),
		Binaries:      binaries,
		ReleaseNotes:  strings.TrimSpace(os.Getenv(prefix + "_RELEASE_NOTES")),
		IsMandatory:   parseBoolEnv(prefix + "_MANDATORY"),
		PublishedAt:   strings.TrimSpace(os.Getenv(prefix + "_PUBLISHED_AT")),
	}
}

func releaseBinaryForPlatform(release ideReleaseRecord, platformKey string) ideReleaseBinary {
	if platformKey != "" {
		if binary, ok := release.Binaries[platformKey]; ok {
			return binary
		}
		normalized := strings.ToLower(strings.ReplaceAll(platformKey, "_", "-"))
		if binary, ok := release.Binaries[normalized]; ok {
			return binary
		}
	}
	for _, key := range []string{"win32-x64", "darwin-arm64", "darwin-x64", "linux-x64"} {
		if binary, ok := release.Binaries[key]; ok {
			return binary
		}
	}
	for _, binary := range release.Binaries {
		return binary
	}
	return ideReleaseBinary{}
}

func normalizeReleaseBinaries(input map[string]ideReleaseBinary) map[string]ideReleaseBinary {
	output := map[string]ideReleaseBinary{}
	for key, binary := range input {
		platformKey := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", "-"))
		if platformKey == "" {
			continue
		}
		binary.URL = strings.TrimSpace(binary.URL)
		binary.SHA256 = strings.TrimSpace(binary.SHA256)
		if binary.URL == "" && binary.SHA256 == "" && binary.Size <= 0 {
			continue
		}
		output[platformKey] = binary
	}
	return output
}

func releaseBinariesToJSON(input map[string]ideReleaseBinary) map[string]map[string]any {
	output := map[string]map[string]any{}
	for key, binary := range input {
		output[key] = map[string]any{
			"url":    binary.URL,
			"sha256": binary.SHA256,
			"size":   binary.Size,
		}
	}
	return output
}

func releaseBinariesFromJSON(input map[string]map[string]any) map[string]ideReleaseBinary {
	output := map[string]ideReleaseBinary{}
	for key, value := range input {
		output[key] = ideReleaseBinary{
			URL:    stringFromJSONMap(value, "url"),
			SHA256: stringFromJSONMap(value, "sha256"),
			Size:   int64FromJSONMap(value, "size"),
		}
	}
	return output
}

func ideReleaseRecordFromEnt(row *dbent.IDERelease) ideReleaseRecord {
	publishedAt := ""
	if !row.PublishedAt.IsZero() {
		publishedAt = row.PublishedAt.UTC().Format(time.RFC3339)
	}
	return ideReleaseRecord{
		Kind:          row.Kind,
		Version:       row.Version,
		LatestVersion: row.Version,
		MinAppVersion: row.MinAppVersion,
		Binaries:      releaseBinariesFromJSON(row.Binaries),
		ReleaseNotes:  row.ReleaseNotes,
		IsMandatory:   row.IsMandatory,
		PublishedAt:   publishedAt,
	}
}

func stringFromJSONMap(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func int64FromJSONMap(values map[string]any, key string) int64 {
	switch value := values[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		if value > 0 {
			return int64(value)
		}
	}
	return 0
}

func nullableReleaseSize(size int64) any {
	if size <= 0 {
		return nil
	}
	return size
}

func normalizeIDEReleasePlatformKey(platformKey string) string {
	replacer := strings.NewReplacer("-", "_", ".", "_", " ", "_")
	return strings.ToUpper(replacer.Replace(strings.TrimSpace(platformKey)))
}

func parseOptionalInt64(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return nil
	}
	return value
}

func parseInt64OrZero(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func parseBoolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

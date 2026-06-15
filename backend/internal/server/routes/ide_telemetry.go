package routes

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	idehandler "github.com/Wei-Shaw/sub2api/internal/handler/ide"

	"github.com/gin-gonic/gin"
)

const (
	ideTelemetryMaxBatchSize = 100
	ideTelemetryDefaultLimit = 50
	ideTelemetryMaxLimit     = 500
)

type ideInstallationHeartbeatRequest struct {
	InstallationID string `json:"installation_id"`
	DeviceID       string `json:"device_id"`
	AppVersion     string `json:"app_version"`
	Platform       string `json:"platform"`
	Arch           string `json:"arch"`
	Channel        string `json:"channel"`
	EngineVersion  string `json:"engine_version"`
	Reason         string `json:"reason"`
}

type ideTelemetryEvent struct {
	Type                  string         `json:"type"`
	Timestamp             string         `json:"timestamp"`
	Severity              string         `json:"severity"`
	Data                  map[string]any `json:"data"`
	AppVersion            string         `json:"appVersion"`
	AppVersionSnake       string         `json:"app_version"`
	Platform              string         `json:"platform"`
	Arch                  string         `json:"arch"`
	EngineVersion         string         `json:"engineVersion"`
	EngineVersionSnake    string         `json:"engine_version"`
	InstallationID        string         `json:"installationId"`
	InstallationIDSnake   string         `json:"installation_id"`
	DeviceID              string         `json:"deviceId"`
	DeviceIDSnake         string         `json:"device_id"`
	ProblemFingerprint    string         `json:"problemFingerprint"`
	ProblemFingerprintRaw string         `json:"problem_fingerprint"`
}

type ideTelemetryRequest struct {
	Events []ideTelemetryEvent `json:"events"`
}

type ideInstallationRecord struct {
	ID             int64   `json:"id"`
	InstallationID string  `json:"installation_id"`
	DeviceID       string  `json:"device_id"`
	UserID         *int64  `json:"user_id,omitempty"`
	AppVersion     string  `json:"app_version"`
	Platform       string  `json:"platform"`
	Arch           string  `json:"arch"`
	Channel        string  `json:"channel"`
	EngineVersion  string  `json:"engine_version"`
	FirstSeenAt    string  `json:"first_seen_at"`
	LastSeenAt     string  `json:"last_seen_at"`
	LastErrorAt    *string `json:"last_error_at,omitempty"`
	Status         string  `json:"status"`
}

type ideProblemRecord struct {
	ID                        int64             `json:"id"`
	ProblemFingerprint        string            `json:"problem_fingerprint"`
	EventType                 string            `json:"event_type"`
	Severity                  string            `json:"severity"`
	Summary                   string            `json:"summary"`
	FirstSeenAt               string            `json:"first_seen_at"`
	LastSeenAt                string            `json:"last_seen_at"`
	LastAppVersion            string            `json:"last_app_version"`
	LastPlatform              string            `json:"last_platform"`
	LastArch                  string            `json:"last_arch"`
	OccurrenceCount           int64             `json:"occurrence_count"`
	AffectedInstallationCount int64             `json:"affected_installation_count"`
	Status                    string            `json:"status"`
	VersionDistribution       map[string]int64  `json:"version_distribution,omitempty"`
	PlatformDistribution      map[string]int64  `json:"platform_distribution,omitempty"`
	RecentEvents              []ideProblemEvent `json:"recent_events,omitempty"`
}

type ideProblemEvent struct {
	ID             int64          `json:"id"`
	InstallationID string         `json:"installation_id"`
	DeviceID       string         `json:"device_id"`
	UserID         *int64         `json:"user_id,omitempty"`
	EventType      string         `json:"event_type"`
	Severity       string         `json:"severity"`
	AppVersion     string         `json:"app_version"`
	Platform       string         `json:"platform"`
	Arch           string         `json:"arch"`
	EngineVersion  string         `json:"engine_version"`
	Summary        string         `json:"summary"`
	Metadata       map[string]any `json:"metadata"`
	OccurredAt     string         `json:"occurred_at"`
}

type ideDistributionItem struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

type ideInstallationStats struct {
	TotalInstallations   int64                 `json:"total_installations"`
	ActiveDevices        int64                 `json:"active_devices"`
	RecentlyActive       int64                 `json:"recently_active"`
	WithErrors           int64                 `json:"with_errors"`
	VersionDistribution  []ideDistributionItem `json:"version_distribution"`
	PlatformDistribution []ideDistributionItem `json:"platform_distribution"`
}

type ideTelemetryNormalizedEvent struct {
	EventType          string
	Severity           string
	AppVersion         string
	Platform           string
	Arch               string
	EngineVersion      string
	InstallationID     string
	DeviceID           string
	Summary            string
	ProblemFingerprint string
	Metadata           map[string]any
	OccurredAt         time.Time
}

type ideTelemetryMemoryState struct {
	sync.Mutex
	nextInstallationID int64
	nextEventID        int64
	nextProblemID      int64
	installations      map[string]ideInstallationRecord
	events             []ideProblemEvent
	problems           map[string]ideProblemRecord
}

var ideTelemetryMemory = ideTelemetryMemoryState{
	nextInstallationID: 1,
	nextEventID:        1,
	nextProblemID:      1,
	installations:      map[string]ideInstallationRecord{},
	events:             []ideProblemEvent{},
	problems:           map[string]ideProblemRecord{},
}

func ideInstallationHeartbeatResponse(entClient *dbent.Client, auth *idehandler.AuthHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ideInstallationHeartbeatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid installation heartbeat payload"})
			return
		}

		req.InstallationID = trimLimit(req.InstallationID, 128)
		req.DeviceID = trimLimit(req.DeviceID, 255)
		req.AppVersion = trimLimit(req.AppVersion, 64)
		req.Platform = trimLimit(req.Platform, 64)
		req.Arch = trimLimit(req.Arch, 64)
		req.Channel = trimLimit(req.Channel, 64)
		req.EngineVersion = trimLimit(req.EngineVersion, 64)
		if req.InstallationID == "" || req.DeviceID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "installation_id and device_id are required"})
			return
		}

		userID, hasUser := optionalIDEUserID(c, auth)
		now := time.Now().UTC()
		if entClient != nil {
			if err := upsertIDEInstallation(c, entClient, req, userID, hasUser, now); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to record IDE installation heartbeat"})
				return
			}
		} else {
			recordIDEInstallationInMemory(req, userID, hasUser, now)
		}

		c.JSON(http.StatusOK, gin.H{
			"code":    0,
			"message": "success",
			"data": gin.H{
				"installation_id": req.InstallationID,
				"device_id":       req.DeviceID,
				"bound_user":      hasUser,
			},
		})
	}
}

func ideTelemetryResponse(entClient *dbent.Client, auth *idehandler.AuthHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var raw json.RawMessage
		if err := c.ShouldBindJSON(&raw); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid telemetry payload"})
			return
		}
		var req ideTelemetryRequest
		if err := json.Unmarshal(raw, &req); err != nil || req.Events == nil {
			if err := json.Unmarshal(raw, &req.Events); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "invalid telemetry payload"})
				return
			}
		}
		if len(req.Events) > ideTelemetryMaxBatchSize {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": 413, "message": "too many telemetry events"})
			return
		}

		userID, hasUser := optionalIDEUserID(c, auth)
		events := make([]ideTelemetryNormalizedEvent, 0, len(req.Events))
		for _, rawEvent := range req.Events {
			normalized, ok := normalizeIDETelemetryEvent(rawEvent)
			if ok {
				events = append(events, normalized)
			}
		}
		if len(events) == 0 {
			c.Status(http.StatusNoContent)
			return
		}

		if entClient != nil {
			if err := recordIDETelemetryEvents(c, entClient, events, userID, hasUser); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to record IDE telemetry"})
				return
			}
		} else {
			recordIDETelemetryInMemory(events, userID, hasUser)
		}

		c.Status(http.StatusNoContent)
	}
}

func ideAdminListInstallations(entClient *dbent.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := boundedQueryInt(c, "limit", ideTelemetryDefaultLimit, 1, ideTelemetryMaxLimit)
		offset := boundedQueryInt(c, "offset", 0, 0, 100000)
		if entClient == nil {
			items, total, stats := listIDEInstallationsFromMemory(c, limit, offset)
			c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{"items": items, "total": total, "stats": stats}})
			return
		}

		where, args := buildIDEInstallationWhere(c)
		items, total, stats, err := queryIDEInstallations(c, entClient, where, args, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to load IDE installations"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{"items": items, "total": total, "stats": stats}})
	}
}

func ideAdminListProblems(entClient *dbent.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := boundedQueryInt(c, "limit", ideTelemetryDefaultLimit, 1, ideTelemetryMaxLimit)
		offset := boundedQueryInt(c, "offset", 0, 0, 100000)
		if entClient == nil {
			items, total := listIDEProblemsFromMemory(c, limit, offset)
			c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{"items": items, "total": total}})
			return
		}

		where, args := buildIDEProblemWhere(c)
		items, total, err := queryIDEProblems(c, entClient, where, args, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to load IDE problem reports"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{"items": items, "total": total}})
	}
}

func ideAdminListProblemEvents(entClient *dbent.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := boundedQueryInt(c, "limit", 20, 1, 100)
		id := strings.TrimSpace(c.Param("id"))
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "message": "problem id is required"})
			return
		}

		if entClient == nil {
			events := listIDEProblemEventsFromMemory(id, limit)
			c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{"items": events, "total": len(events)}})
			return
		}

		events, err := queryIDEProblemEvents(c, entClient, id, limit)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "failed to load IDE problem events"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 0, "message": "success", "data": gin.H{"items": events, "total": len(events)}})
	}
}

func optionalIDEUserID(c *gin.Context, auth *idehandler.AuthHandler) (int64, bool) {
	if auth == nil {
		return 0, false
	}
	return auth.UserIDFromAuthorizationHeader(c.Request.Context(), c.GetHeader("Authorization"))
}

func upsertIDEInstallation(c *gin.Context, entClient *dbent.Client, req ideInstallationHeartbeatRequest, userID int64, hasUser bool, now time.Time) error {
	var userValue any
	if hasUser {
		userValue = userID
	}
	_, err := entClient.ExecContext(c.Request.Context(), `
		INSERT INTO ide_installations (
			installation_id, device_id, user_id, app_version, platform, arch, channel, engine_version,
			first_seen_at, last_seen_at, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, 'active', $9, $9)
		ON CONFLICT (installation_id) DO UPDATE SET
			device_id = EXCLUDED.device_id,
			user_id = COALESCE(EXCLUDED.user_id, ide_installations.user_id),
			app_version = EXCLUDED.app_version,
			platform = EXCLUDED.platform,
			arch = EXCLUDED.arch,
			channel = EXCLUDED.channel,
			engine_version = EXCLUDED.engine_version,
			last_seen_at = EXCLUDED.last_seen_at,
			status = 'active',
			updated_at = EXCLUDED.updated_at
	`, req.InstallationID, req.DeviceID, userValue, req.AppVersion, req.Platform, req.Arch, req.Channel, req.EngineVersion, now)
	return err
}

func recordIDETelemetryEvents(c *gin.Context, entClient *dbent.Client, events []ideTelemetryNormalizedEvent, userID int64, hasUser bool) error {
	for _, event := range events {
		metadataJSON, err := json.Marshal(event.Metadata)
		if err != nil {
			return err
		}
		var userValue any
		if hasUser {
			userValue = userID
		}
		_, err = entClient.ExecContext(c.Request.Context(), `
			INSERT INTO ide_telemetry_events (
				installation_id, device_id, user_id, event_type, severity, app_version, platform, arch,
				engine_version, summary, problem_fingerprint, metadata, occurred_at, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, NOW())
		`, event.InstallationID, event.DeviceID, userValue, event.EventType, event.Severity, event.AppVersion, event.Platform, event.Arch, event.EngineVersion, event.Summary, event.ProblemFingerprint, string(metadataJSON), event.OccurredAt)
		if err != nil {
			return err
		}
		if event.InstallationID != "" {
			_, _ = entClient.ExecContext(c.Request.Context(), `
				UPDATE ide_installations
				SET last_seen_at = GREATEST(last_seen_at, $2),
					last_error_at = CASE WHEN $3 THEN GREATEST(COALESCE(last_error_at, $2), $2) ELSE last_error_at END,
					updated_at = NOW()
				WHERE installation_id = $1
			`, event.InstallationID, event.OccurredAt, isIDEProblemEvent(event))
		}
		if isIDEProblemEvent(event) {
			if err := upsertIDEProblemReport(c, entClient, event); err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertIDEProblemReport(c *gin.Context, entClient *dbent.Client, event ideTelemetryNormalizedEvent) error {
	if event.ProblemFingerprint == "" {
		return nil
	}
	_, err := entClient.ExecContext(c.Request.Context(), `
		INSERT INTO ide_problem_reports (
			problem_fingerprint, event_type, severity, summary, first_seen_at, last_seen_at,
			last_app_version, last_platform, last_arch, occurrence_count, affected_installation_count, status,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $8, 1,
			(SELECT COUNT(DISTINCT installation_id) FROM ide_telemetry_events WHERE problem_fingerprint = $1 AND installation_id <> ''),
			'open', NOW(), NOW()
		)
		ON CONFLICT (problem_fingerprint) DO UPDATE SET
			event_type = EXCLUDED.event_type,
			severity = EXCLUDED.severity,
			summary = EXCLUDED.summary,
			last_seen_at = GREATEST(ide_problem_reports.last_seen_at, EXCLUDED.last_seen_at),
			last_app_version = EXCLUDED.last_app_version,
			last_platform = EXCLUDED.last_platform,
			last_arch = EXCLUDED.last_arch,
			occurrence_count = ide_problem_reports.occurrence_count + 1,
			affected_installation_count = (
				SELECT COUNT(DISTINCT installation_id)
				FROM ide_telemetry_events
				WHERE problem_fingerprint = EXCLUDED.problem_fingerprint AND installation_id <> ''
			),
			updated_at = NOW()
	`, event.ProblemFingerprint, event.EventType, event.Severity, event.Summary, event.OccurredAt, event.AppVersion, event.Platform, event.Arch)
	return err
}

func queryIDEInstallations(c *gin.Context, entClient *dbent.Client, where string, args []any, limit, offset int) ([]ideInstallationRecord, int, ideInstallationStats, error) {
	total, err := queryCount(c, entClient, "SELECT COUNT(*) FROM ide_installations "+where, args...)
	if err != nil {
		return nil, 0, ideInstallationStats{}, err
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := entClient.QueryContext(c.Request.Context(), fmt.Sprintf(`
		SELECT id, installation_id, device_id, user_id, app_version, platform, arch, channel, engine_version,
			first_seen_at, last_seen_at, last_error_at, status
		FROM ide_installations
		%s
		ORDER BY last_seen_at DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)+1, len(args)+2), queryArgs...)
	if err != nil {
		return nil, 0, ideInstallationStats{}, err
	}
	defer rows.Close()

	items := []ideInstallationRecord{}
	for rows.Next() {
		var item ideInstallationRecord
		var user sql.NullInt64
		var firstSeen, lastSeen time.Time
		var lastError sql.NullTime
		if err := rows.Scan(&item.ID, &item.InstallationID, &item.DeviceID, &user, &item.AppVersion, &item.Platform, &item.Arch, &item.Channel, &item.EngineVersion, &firstSeen, &lastSeen, &lastError, &item.Status); err != nil {
			return nil, 0, ideInstallationStats{}, err
		}
		if user.Valid {
			item.UserID = &user.Int64
		}
		item.FirstSeenAt = firstSeen.UTC().Format(time.RFC3339)
		item.LastSeenAt = lastSeen.UTC().Format(time.RFC3339)
		if lastError.Valid {
			value := lastError.Time.UTC().Format(time.RFC3339)
			item.LastErrorAt = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, ideInstallationStats{}, err
	}
	stats, err := queryIDEInstallationStats(c, entClient, where, args)
	if err != nil {
		return nil, 0, ideInstallationStats{}, err
	}
	return items, total, stats, nil
}

func queryIDEInstallationStats(c *gin.Context, entClient *dbent.Client, where string, args []any) (ideInstallationStats, error) {
	stats := ideInstallationStats{}
	rows, err := entClient.QueryContext(c.Request.Context(), `
		SELECT COUNT(*), COUNT(DISTINCT NULLIF(device_id, '')),
			COUNT(*) FILTER (WHERE last_seen_at >= NOW() - INTERVAL '30 days'),
			COUNT(*) FILTER (WHERE last_error_at IS NOT NULL)
		FROM ide_installations `+where, args...)
	if err != nil {
		return stats, err
	}
	if rows.Next() {
		if err := rows.Scan(&stats.TotalInstallations, &stats.ActiveDevices, &stats.RecentlyActive, &stats.WithErrors); err != nil {
			rows.Close()
			return stats, err
		}
	}
	rows.Close()
	versionDistribution, err := queryDistribution(c, entClient, "ide_installations", "app_version", where, args)
	if err != nil {
		return stats, err
	}
	platformDistribution, err := queryDistribution(c, entClient, "ide_installations", "platform", where, args)
	if err != nil {
		return stats, err
	}
	stats.VersionDistribution = versionDistribution
	stats.PlatformDistribution = platformDistribution
	return stats, nil
}

func queryIDEProblems(c *gin.Context, entClient *dbent.Client, where string, args []any, limit, offset int) ([]ideProblemRecord, int, error) {
	total, err := queryCount(c, entClient, "SELECT COUNT(*) FROM ide_problem_reports "+where, args...)
	if err != nil {
		return nil, 0, err
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, err := entClient.QueryContext(c.Request.Context(), fmt.Sprintf(`
		SELECT id, problem_fingerprint, event_type, severity, summary, first_seen_at, last_seen_at,
			last_app_version, last_platform, last_arch, occurrence_count, affected_installation_count, status
		FROM ide_problem_reports
		%s
		ORDER BY last_seen_at DESC
		LIMIT $%d OFFSET $%d
	`, where, len(args)+1, len(args)+2), queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []ideProblemRecord{}
	for rows.Next() {
		var item ideProblemRecord
		var firstSeen, lastSeen time.Time
		if err := rows.Scan(&item.ID, &item.ProblemFingerprint, &item.EventType, &item.Severity, &item.Summary, &firstSeen, &lastSeen, &item.LastAppVersion, &item.LastPlatform, &item.LastArch, &item.OccurrenceCount, &item.AffectedInstallationCount, &item.Status); err != nil {
			return nil, 0, err
		}
		item.FirstSeenAt = firstSeen.UTC().Format(time.RFC3339)
		item.LastSeenAt = lastSeen.UTC().Format(time.RFC3339)
		item.VersionDistribution = queryProblemEventDistribution(c, entClient, item.ProblemFingerprint, "app_version")
		item.PlatformDistribution = queryProblemEventDistribution(c, entClient, item.ProblemFingerprint, "platform")
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func queryIDEProblemEvents(c *gin.Context, entClient *dbent.Client, id string, limit int) ([]ideProblemEvent, error) {
	fingerprint := id
	if numericID, err := strconv.ParseInt(id, 10, 64); err == nil {
		rows, err := entClient.QueryContext(c.Request.Context(), `SELECT problem_fingerprint FROM ide_problem_reports WHERE id = $1`, numericID)
		if err != nil {
			return nil, err
		}
		if rows.Next() {
			if err := rows.Scan(&fingerprint); err != nil {
				rows.Close()
				return nil, err
			}
		}
		rows.Close()
	}

	rows, err := entClient.QueryContext(c.Request.Context(), `
		SELECT id, installation_id, device_id, user_id, event_type, severity, app_version, platform, arch,
			engine_version, summary, metadata, occurred_at
		FROM ide_telemetry_events
		WHERE problem_fingerprint = $1
		ORDER BY occurred_at DESC
		LIMIT $2
	`, fingerprint, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ideProblemEvent{}
	for rows.Next() {
		item, err := scanIDEProblemEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func queryCount(c *gin.Context, entClient *dbent.Client, query string, args ...any) (int, error) {
	rows, err := entClient.QueryContext(c.Request.Context(), query, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total int
	if rows.Next() {
		if err := rows.Scan(&total); err != nil {
			return 0, err
		}
	}
	return total, rows.Err()
}

func queryDistribution(c *gin.Context, entClient *dbent.Client, table, column, where string, args []any) ([]ideDistributionItem, error) {
	rows, err := entClient.QueryContext(c.Request.Context(), fmt.Sprintf(`
		SELECT COALESCE(NULLIF(%s, ''), 'unknown') AS name, COUNT(*) AS count
		FROM %s
		%s
		GROUP BY name
		ORDER BY count DESC, name ASC
		LIMIT 12
	`, column, table, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ideDistributionItem{}
	for rows.Next() {
		var item ideDistributionItem
		if err := rows.Scan(&item.Name, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func queryProblemEventDistribution(c *gin.Context, entClient *dbent.Client, fingerprint, column string) map[string]int64 {
	rows, err := entClient.QueryContext(c.Request.Context(), fmt.Sprintf(`
		SELECT COALESCE(NULLIF(%s, ''), 'unknown') AS name, COUNT(*) AS count
		FROM ide_telemetry_events
		WHERE problem_fingerprint = $1
		GROUP BY name
		ORDER BY count DESC, name ASC
		LIMIT 10
	`, column), fingerprint)
	if err != nil {
		return map[string]int64{}
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var name string
		var count int64
		if err := rows.Scan(&name, &count); err == nil {
			out[name] = count
		}
	}
	return out
}

type ideProblemEventScanner interface {
	Scan(dest ...any) error
}

func scanIDEProblemEvent(scanner ideProblemEventScanner) (ideProblemEvent, error) {
	var item ideProblemEvent
	var user sql.NullInt64
	var metadataRaw []byte
	var occurredAt time.Time
	if err := scanner.Scan(&item.ID, &item.InstallationID, &item.DeviceID, &user, &item.EventType, &item.Severity, &item.AppVersion, &item.Platform, &item.Arch, &item.EngineVersion, &item.Summary, &metadataRaw, &occurredAt); err != nil {
		return item, err
	}
	if user.Valid {
		item.UserID = &user.Int64
	}
	item.Metadata = map[string]any{}
	_ = json.Unmarshal(metadataRaw, &item.Metadata)
	item.OccurredAt = occurredAt.UTC().Format(time.RFC3339)
	return item, nil
}

func buildIDEInstallationWhere(c *gin.Context) (string, []any) {
	clauses := []string{}
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if value := strings.TrimSpace(c.Query("user_id")); value != "" {
		if id, err := strconv.ParseInt(value, 10, 64); err == nil {
			add("user_id = $%d", id)
		}
	}
	if value := trimLimit(c.Query("version"), 64); value != "" {
		add("app_version = $%d", value)
	}
	if value := trimLimit(c.Query("platform"), 64); value != "" {
		add("platform = $%d", value)
	}
	if value := trimLimit(c.Query("status"), 32); value != "" {
		add("status = $%d", value)
	}
	if strings.EqualFold(c.Query("has_error"), "true") {
		clauses = append(clauses, "last_error_at IS NOT NULL")
	}
	if days := boundedQueryInt(c, "active_days", 0, 0, 3650); days > 0 {
		args = append(args, days)
		clauses = append(clauses, fmt.Sprintf("last_seen_at >= NOW() - ($%d::int * INTERVAL '1 day')", len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func buildIDEProblemWhere(c *gin.Context) (string, []any) {
	clauses := []string{}
	args := []any{}
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if value := trimLimit(c.Query("event_type"), 64); value != "" {
		add("event_type = $%d", value)
	}
	if value := trimLimit(c.Query("platform"), 64); value != "" {
		add("last_platform = $%d", value)
	}
	if value := trimLimit(c.Query("version"), 64); value != "" {
		add("last_app_version = $%d", value)
	}
	if value := trimLimit(c.Query("status"), 32); value != "" {
		add("status = $%d", value)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func normalizeIDETelemetryEvent(raw ideTelemetryEvent) (ideTelemetryNormalizedEvent, bool) {
	eventType := trimLimit(raw.Type, 64)
	if eventType == "" {
		return ideTelemetryNormalizedEvent{}, false
	}
	occurredAt := time.Now().UTC()
	if raw.Timestamp != "" {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw.Timestamp)); err == nil {
			occurredAt = parsed.UTC()
		}
	}
	appVersion := firstNonEmpty(raw.AppVersion, raw.AppVersionSnake)
	engineVersion := firstNonEmpty(raw.EngineVersion, raw.EngineVersionSnake)
	installationID := firstNonEmpty(raw.InstallationID, raw.InstallationIDSnake)
	deviceID := firstNonEmpty(raw.DeviceID, raw.DeviceIDSnake)
	severity := normalizeIDESeverity(raw.Severity, eventType)
	metadata := sanitizeIDEMetadata(raw.Data)
	summary := buildIDETelemetrySummary(eventType, metadata)
	fingerprint := firstNonEmpty(raw.ProblemFingerprint, raw.ProblemFingerprintRaw)
	if fingerprint == "" && isProblemEventType(eventType, severity) {
		fingerprint = fingerprintIDETelemetry(eventType, metadata, summary)
	}

	return ideTelemetryNormalizedEvent{
		EventType:          eventType,
		Severity:           severity,
		AppVersion:         trimLimit(appVersion, 64),
		Platform:           trimLimit(raw.Platform, 64),
		Arch:               trimLimit(raw.Arch, 64),
		EngineVersion:      trimLimit(engineVersion, 64),
		InstallationID:     trimLimit(installationID, 128),
		DeviceID:           trimLimit(deviceID, 255),
		Summary:            trimLimit(summary, 500),
		ProblemFingerprint: trimLimit(fingerprint, 128),
		Metadata:           metadata,
		OccurredAt:         occurredAt,
	}, true
}

func normalizeIDESeverity(raw, eventType string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "debug", "info", "warn", "warning", "error", "fatal":
		if value == "warning" {
			return "warn"
		}
		return value
	}
	if isProblemEventType(eventType, "") {
		return "error"
	}
	return "info"
}

func sanitizeIDEMetadata(input map[string]any) map[string]any {
	output := map[string]any{}
	for key, value := range input {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" ||
			strings.Contains(normalizedKey, "token") ||
			strings.Contains(normalizedKey, "jwt") ||
			strings.Contains(normalizedKey, "secret") ||
			strings.Contains(normalizedKey, "prompt") ||
			strings.Contains(normalizedKey, "code") ||
			strings.Contains(normalizedKey, "path") ||
			strings.Contains(normalizedKey, "log") {
			continue
		}
		output[key] = sanitizeIDEMetadataValue(value)
	}
	return output
}

func sanitizeIDEMetadataValue(value any) any {
	switch typed := value.(type) {
	case string:
		return trimLimit(typed, 500)
	case float64, bool, nil:
		return typed
	case []any:
		if len(typed) > 20 {
			typed = typed[:20]
		}
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeIDEMetadataValue(item))
		}
		return out
	case map[string]any:
		return sanitizeIDEMetadata(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func buildIDETelemetrySummary(eventType string, metadata map[string]any) string {
	for _, key := range []string{"summary", "message", "error", "reason"} {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			return normalizeErrorSummary(value)
		}
	}
	component := stringMetadataValue(metadata, "component")
	errorType := firstNonEmpty(stringMetadataValue(metadata, "errorType"), stringMetadataValue(metadata, "error_type"), stringMetadataValue(metadata, "name"))
	parts := []string{eventType}
	if component != "" {
		parts = append(parts, component)
	}
	if errorType != "" {
		parts = append(parts, errorType)
	}
	return normalizeErrorSummary(strings.Join(parts, ": "))
}

func fingerprintIDETelemetry(eventType string, metadata map[string]any, summary string) string {
	component := stringMetadataValue(metadata, "component")
	errorType := firstNonEmpty(stringMetadataValue(metadata, "errorType"), stringMetadataValue(metadata, "error_type"), stringMetadataValue(metadata, "name"))
	raw := strings.Join([]string{eventType, component, errorType, normalizeErrorSummary(summary)}, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])[:32]
}

func isIDEProblemEvent(event ideTelemetryNormalizedEvent) bool {
	return isProblemEventType(event.EventType, event.Severity)
}

func isProblemEventType(eventType, severity string) bool {
	lowerType := strings.ToLower(eventType)
	lowerSeverity := strings.ToLower(severity)
	return lowerSeverity == "error" || lowerSeverity == "fatal" ||
		strings.Contains(lowerType, "failed") ||
		strings.Contains(lowerType, "crash") ||
		strings.Contains(lowerType, "error") ||
		strings.Contains(lowerType, "integrity")
}

func normalizeErrorSummary(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	return trimLimit(value, 500)
}

func stringMetadataValue(metadata map[string]any, key string) string {
	if value, ok := metadata[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func trimLimit(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func boundedQueryInt(c *gin.Context, key string, fallback, min, max int) int {
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(key)))
	if err != nil {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func recordIDEInstallationInMemory(req ideInstallationHeartbeatRequest, userID int64, hasUser bool, now time.Time) {
	ideTelemetryMemory.Lock()
	defer ideTelemetryMemory.Unlock()
	record, ok := ideTelemetryMemory.installations[req.InstallationID]
	if !ok {
		record.ID = ideTelemetryMemory.nextInstallationID
		ideTelemetryMemory.nextInstallationID++
		record.InstallationID = req.InstallationID
		record.FirstSeenAt = now.Format(time.RFC3339)
		record.Status = "active"
	}
	record.DeviceID = req.DeviceID
	if hasUser {
		record.UserID = &userID
	}
	record.AppVersion = req.AppVersion
	record.Platform = req.Platform
	record.Arch = req.Arch
	record.Channel = req.Channel
	record.EngineVersion = req.EngineVersion
	record.LastSeenAt = now.Format(time.RFC3339)
	ideTelemetryMemory.installations[req.InstallationID] = record
}

func recordIDETelemetryInMemory(events []ideTelemetryNormalizedEvent, userID int64, hasUser bool) {
	ideTelemetryMemory.Lock()
	defer ideTelemetryMemory.Unlock()
	for _, event := range events {
		var userPtr *int64
		if hasUser {
			value := userID
			userPtr = &value
		}
		item := ideProblemEvent{
			ID:             ideTelemetryMemory.nextEventID,
			InstallationID: event.InstallationID,
			DeviceID:       event.DeviceID,
			UserID:         userPtr,
			EventType:      event.EventType,
			Severity:       event.Severity,
			AppVersion:     event.AppVersion,
			Platform:       event.Platform,
			Arch:           event.Arch,
			EngineVersion:  event.EngineVersion,
			Summary:        event.Summary,
			Metadata:       event.Metadata,
			OccurredAt:     event.OccurredAt.Format(time.RFC3339),
		}
		ideTelemetryMemory.nextEventID++
		ideTelemetryMemory.events = append(ideTelemetryMemory.events, item)
		if install, ok := ideTelemetryMemory.installations[event.InstallationID]; ok {
			install.LastSeenAt = event.OccurredAt.Format(time.RFC3339)
			if isIDEProblemEvent(event) {
				value := event.OccurredAt.Format(time.RFC3339)
				install.LastErrorAt = &value
			}
			ideTelemetryMemory.installations[event.InstallationID] = install
		}
		if !isIDEProblemEvent(event) || event.ProblemFingerprint == "" {
			continue
		}
		problem := ideTelemetryMemory.problems[event.ProblemFingerprint]
		if problem.ID == 0 {
			problem.ID = ideTelemetryMemory.nextProblemID
			ideTelemetryMemory.nextProblemID++
			problem.ProblemFingerprint = event.ProblemFingerprint
			problem.FirstSeenAt = event.OccurredAt.Format(time.RFC3339)
			problem.Status = "open"
		}
		problem.EventType = event.EventType
		problem.Severity = event.Severity
		problem.Summary = event.Summary
		problem.LastSeenAt = event.OccurredAt.Format(time.RFC3339)
		problem.LastAppVersion = event.AppVersion
		problem.LastPlatform = event.Platform
		problem.LastArch = event.Arch
		problem.OccurrenceCount++
		problem.AffectedInstallationCount = countMemoryAffectedInstallations(event.ProblemFingerprint)
		ideTelemetryMemory.problems[event.ProblemFingerprint] = problem
	}
}

func listIDEInstallationsFromMemory(c *gin.Context, limit, offset int) ([]ideInstallationRecord, int, ideInstallationStats) {
	ideTelemetryMemory.Lock()
	defer ideTelemetryMemory.Unlock()
	all := make([]ideInstallationRecord, 0, len(ideTelemetryMemory.installations))
	for _, item := range ideTelemetryMemory.installations {
		if matchesMemoryInstallationFilters(c, item) {
			all = append(all, item)
		}
	}
	sortInstallationsByLastSeen(all)
	total := len(all)
	end := offset + limit
	if offset > len(all) {
		offset = len(all)
	}
	if end > len(all) {
		end = len(all)
	}
	stats := buildMemoryInstallationStats(all)
	return all[offset:end], total, stats
}

func listIDEProblemsFromMemory(c *gin.Context, limit, offset int) ([]ideProblemRecord, int) {
	ideTelemetryMemory.Lock()
	defer ideTelemetryMemory.Unlock()
	all := make([]ideProblemRecord, 0, len(ideTelemetryMemory.problems))
	for _, item := range ideTelemetryMemory.problems {
		if matchesMemoryProblemFilters(c, item) {
			copy := item
			copy.VersionDistribution = memoryProblemDistribution(item.ProblemFingerprint, "version")
			copy.PlatformDistribution = memoryProblemDistribution(item.ProblemFingerprint, "platform")
			all = append(all, copy)
		}
	}
	sortProblemsByLastSeen(all)
	total := len(all)
	end := offset + limit
	if offset > len(all) {
		offset = len(all)
	}
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total
}

func listIDEProblemEventsFromMemory(id string, limit int) []ideProblemEvent {
	ideTelemetryMemory.Lock()
	defer ideTelemetryMemory.Unlock()
	fingerprint := id
	if numericID, err := strconv.ParseInt(id, 10, 64); err == nil {
		for _, problem := range ideTelemetryMemory.problems {
			if problem.ID == numericID {
				fingerprint = problem.ProblemFingerprint
				break
			}
		}
	}
	out := []ideProblemEvent{}
	for i := len(ideTelemetryMemory.events) - 1; i >= 0 && len(out) < limit; i-- {
		event := ideTelemetryMemory.events[i]
		if fingerprintIDETelemetry(event.EventType, event.Metadata, event.Summary) == fingerprint {
			out = append(out, event)
		}
	}
	return out
}

func countMemoryAffectedInstallations(fingerprint string) int64 {
	seen := map[string]struct{}{}
	for _, event := range ideTelemetryMemory.events {
		if event.InstallationID == "" {
			continue
		}
		if fingerprintIDETelemetry(event.EventType, event.Metadata, event.Summary) == fingerprint {
			seen[event.InstallationID] = struct{}{}
		}
	}
	return int64(len(seen))
}

func buildMemoryInstallationStats(items []ideInstallationRecord) ideInstallationStats {
	stats := ideInstallationStats{TotalInstallations: int64(len(items))}
	devices := map[string]struct{}{}
	versions := map[string]int64{}
	platforms := map[string]int64{}
	now := time.Now().UTC()
	for _, item := range items {
		if item.DeviceID != "" {
			devices[item.DeviceID] = struct{}{}
		}
		if parsed, err := time.Parse(time.RFC3339, item.LastSeenAt); err == nil && now.Sub(parsed) <= 30*24*time.Hour {
			stats.RecentlyActive++
		}
		if item.LastErrorAt != nil {
			stats.WithErrors++
		}
		versions[firstNonEmpty(item.AppVersion, "unknown")]++
		platforms[firstNonEmpty(item.Platform, "unknown")]++
	}
	stats.ActiveDevices = int64(len(devices))
	stats.VersionDistribution = mapToDistributionItems(versions)
	stats.PlatformDistribution = mapToDistributionItems(platforms)
	return stats
}

func matchesMemoryInstallationFilters(c *gin.Context, item ideInstallationRecord) bool {
	if value := strings.TrimSpace(c.Query("user_id")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || item.UserID == nil || *item.UserID != id {
			return false
		}
	}
	if value := trimLimit(c.Query("version"), 64); value != "" && item.AppVersion != value {
		return false
	}
	if value := trimLimit(c.Query("platform"), 64); value != "" && item.Platform != value {
		return false
	}
	if value := trimLimit(c.Query("status"), 32); value != "" && item.Status != value {
		return false
	}
	if strings.EqualFold(c.Query("has_error"), "true") && item.LastErrorAt == nil {
		return false
	}
	if days := boundedQueryInt(c, "active_days", 0, 0, 3650); days > 0 {
		parsed, err := time.Parse(time.RFC3339, item.LastSeenAt)
		if err != nil || time.Since(parsed) > time.Duration(days)*24*time.Hour {
			return false
		}
	}
	return true
}

func matchesMemoryProblemFilters(c *gin.Context, item ideProblemRecord) bool {
	if value := trimLimit(c.Query("event_type"), 64); value != "" && item.EventType != value {
		return false
	}
	if value := trimLimit(c.Query("platform"), 64); value != "" && item.LastPlatform != value {
		return false
	}
	if value := trimLimit(c.Query("version"), 64); value != "" && item.LastAppVersion != value {
		return false
	}
	if value := trimLimit(c.Query("status"), 32); value != "" && item.Status != value {
		return false
	}
	return true
}

func sortInstallationsByLastSeen(items []ideInstallationRecord) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].LastSeenAt > items[j].LastSeenAt
	})
}

func sortProblemsByLastSeen(items []ideProblemRecord) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].LastSeenAt > items[j].LastSeenAt
	})
}

func mapToDistributionItems(input map[string]int64) []ideDistributionItem {
	items := make([]ideDistributionItem, 0, len(input))
	for name, count := range input {
		items = append(items, ideDistributionItem{Name: name, Count: count})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Name < items[j].Name
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > 12 {
		return items[:12]
	}
	return items
}

func memoryProblemDistribution(fingerprint, kind string) map[string]int64 {
	out := map[string]int64{}
	for _, event := range ideTelemetryMemory.events {
		if fingerprintIDETelemetry(event.EventType, event.Metadata, event.Summary) != fingerprint {
			continue
		}
		name := "unknown"
		switch kind {
		case "version":
			name = firstNonEmpty(event.AppVersion, "unknown")
		case "platform":
			name = firstNonEmpty(event.Platform, "unknown")
		}
		out[name]++
	}
	return out
}

func resetIDETelemetryMemoryForTest() {
	ideTelemetryMemory.Lock()
	defer ideTelemetryMemory.Unlock()
	ideTelemetryMemory.nextInstallationID = 1
	ideTelemetryMemory.nextEventID = 1
	ideTelemetryMemory.nextProblemID = 1
	ideTelemetryMemory.installations = map[string]ideInstallationRecord{}
	ideTelemetryMemory.events = []ideProblemEvent{}
	ideTelemetryMemory.problems = map[string]ideProblemRecord{}
}

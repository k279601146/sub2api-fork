-- IDE 安装、遥测事件和问题聚合表。
-- 仅保存安装/版本/平台/有限错误摘要，不保存代码、提示词、路径、密钥或完整日志。

CREATE TABLE IF NOT EXISTS ide_installations (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    installation_id VARCHAR(128) NOT NULL UNIQUE,
    device_id VARCHAR(255) NOT NULL DEFAULT '',
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    app_version VARCHAR(64) NOT NULL DEFAULT '',
    platform VARCHAR(64) NOT NULL DEFAULT '',
    arch VARCHAR(64) NOT NULL DEFAULT '',
    channel VARCHAR(64) NOT NULL DEFAULT '',
    engine_version VARCHAR(64) NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error_at TIMESTAMPTZ,
    status VARCHAR(32) NOT NULL DEFAULT 'active'
);

CREATE INDEX IF NOT EXISTS idx_ide_installations_user_id ON ide_installations(user_id);
CREATE INDEX IF NOT EXISTS idx_ide_installations_last_seen_at ON ide_installations(last_seen_at);
CREATE INDEX IF NOT EXISTS idx_ide_installations_platform ON ide_installations(platform);
CREATE INDEX IF NOT EXISTS idx_ide_installations_app_version ON ide_installations(app_version);
CREATE INDEX IF NOT EXISTS idx_ide_installations_status ON ide_installations(status);

CREATE TABLE IF NOT EXISTS ide_telemetry_events (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    installation_id VARCHAR(128) NOT NULL DEFAULT '',
    device_id VARCHAR(255) NOT NULL DEFAULT '',
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    event_type VARCHAR(64) NOT NULL,
    severity VARCHAR(16) NOT NULL DEFAULT 'info',
    app_version VARCHAR(64) NOT NULL DEFAULT '',
    platform VARCHAR(64) NOT NULL DEFAULT '',
    arch VARCHAR(64) NOT NULL DEFAULT '',
    engine_version VARCHAR(64) NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    problem_fingerprint VARCHAR(128) NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ide_telemetry_events_occurred_at ON ide_telemetry_events(occurred_at);
CREATE INDEX IF NOT EXISTS idx_ide_telemetry_events_installation_id ON ide_telemetry_events(installation_id);
CREATE INDEX IF NOT EXISTS idx_ide_telemetry_events_event_type ON ide_telemetry_events(event_type);
CREATE INDEX IF NOT EXISTS idx_ide_telemetry_events_problem ON ide_telemetry_events(problem_fingerprint);

CREATE TABLE IF NOT EXISTS ide_problem_reports (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    problem_fingerprint VARCHAR(128) NOT NULL UNIQUE,
    event_type VARCHAR(64) NOT NULL,
    severity VARCHAR(16) NOT NULL DEFAULT 'error',
    summary TEXT NOT NULL DEFAULT '',
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_app_version VARCHAR(64) NOT NULL DEFAULT '',
    last_platform VARCHAR(64) NOT NULL DEFAULT '',
    last_arch VARCHAR(64) NOT NULL DEFAULT '',
    occurrence_count BIGINT NOT NULL DEFAULT 0,
    affected_installation_count BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'open'
);

CREATE INDEX IF NOT EXISTS idx_ide_problem_reports_last_seen_at ON ide_problem_reports(last_seen_at);
CREATE INDEX IF NOT EXISTS idx_ide_problem_reports_status ON ide_problem_reports(status);

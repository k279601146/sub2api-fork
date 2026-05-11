-- IDE client integration tables.
-- These tables are append-only extensions for desktop OAuth sessions and app/engine releases.

CREATE TABLE IF NOT EXISTS ide_sessions (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    session_id VARCHAR(128) NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    jwt_token_hash VARCHAR(128) NOT NULL UNIQUE,
    client_id VARCHAR(128) NOT NULL DEFAULT 'myide-desktop',
    client_version VARCHAR(64) NOT NULL DEFAULT '',
    platform VARCHAR(64) NOT NULL DEFAULT '',
    device_id VARCHAR(255) NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    last_used_at TIMESTAMPTZ NOT NULL,
    revoked BOOLEAN NOT NULL DEFAULT FALSE,
    revoke_reason VARCHAR(64) NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_ide_sessions_user_revoked ON ide_sessions(user_id, revoked);
CREATE INDEX IF NOT EXISTS idx_ide_sessions_expires_at ON ide_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_ide_sessions_last_used_at ON ide_sessions(last_used_at);

CREATE TABLE IF NOT EXISTS ide_releases (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    kind VARCHAR(20) NOT NULL,
    version VARCHAR(64) NOT NULL,
    min_app_version VARCHAR(64) NOT NULL DEFAULT '',
    binaries JSONB NOT NULL DEFAULT '{}'::jsonb,
    release_notes TEXT NOT NULL DEFAULT '',
    is_mandatory BOOLEAN NOT NULL DEFAULT FALSE,
    is_latest BOOLEAN NOT NULL DEFAULT TRUE,
    published_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ide_releases_kind_version ON ide_releases(kind, version);
CREATE INDEX IF NOT EXISTS idx_ide_releases_kind_latest ON ide_releases(kind, is_latest);
CREATE INDEX IF NOT EXISTS idx_ide_releases_published_at ON ide_releases(published_at);

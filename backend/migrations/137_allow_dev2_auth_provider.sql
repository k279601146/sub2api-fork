ALTER TABLE auth_identities
    DROP CONSTRAINT IF EXISTS auth_identities_provider_type_check;

ALTER TABLE auth_identities
    ADD CONSTRAINT auth_identities_provider_type_check
    CHECK (provider_type IN ('email', 'dev2', 'linuxdo', 'wechat', 'oidc', 'github', 'google'));

ALTER TABLE auth_identity_channels
    DROP CONSTRAINT IF EXISTS auth_identity_channels_provider_type_check;

ALTER TABLE auth_identity_channels
    ADD CONSTRAINT auth_identity_channels_provider_type_check
    CHECK (provider_type IN ('email', 'dev2', 'linuxdo', 'wechat', 'oidc', 'github', 'google'));

ALTER TABLE pending_auth_sessions
    DROP CONSTRAINT IF EXISTS pending_auth_sessions_provider_type_check;

ALTER TABLE pending_auth_sessions
    ADD CONSTRAINT pending_auth_sessions_provider_type_check
    CHECK (provider_type IN ('email', 'dev2', 'linuxdo', 'wechat', 'oidc', 'github', 'google'));

//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type bmSettingRepo struct {
	values map[string]string
}

func (r *bmSettingRepo) Get(_ context.Context, _ string) (*service.Setting, error) {
	panic("unexpected Get call")
}

func (r *bmSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	v, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return v, nil
}

func (r *bmSettingRepo) Set(_ context.Context, _, _ string) error {
	panic("unexpected Set call")
}

func (r *bmSettingRepo) GetMultiple(_ context.Context, _ []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (r *bmSettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	if r.values == nil {
		r.values = make(map[string]string, len(settings))
	}
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *bmSettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (r *bmSettingRepo) Delete(_ context.Context, _ string) error {
	panic("unexpected Delete call")
}

func newBackendModeSettingService(t *testing.T, enabled string) *service.SettingService {
	t.Helper()

	repo := &bmSettingRepo{
		values: map[string]string{
			service.SettingKeyBackendModeEnabled: enabled,
		},
	}
	svc := service.NewSettingService(repo, &config.Config{})
	require.NoError(t, svc.UpdateSettings(context.Background(), &service.SystemSettings{
		BackendModeEnabled: enabled == "true",
	}))

	return svc
}

func stringPtr(v string) *string {
	return &v
}

func TestBackendModeUserGuard(t *testing.T) {
	tests := []struct {
		name       string
		nilService bool
		enabled    string
		role       *string
		subject    *AuthSubject
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "disabled_allows_all",
			enabled:    "false",
			role:       stringPtr("user"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil_service_allows_all",
			nilService: true,
			role:       stringPtr("user"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_admin_allowed",
			enabled:    "true",
			role:       stringPtr("admin"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_user_blocked",
			enabled:    "true",
			role:       stringPtr("user"),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_no_role_blocked",
			enabled:    "true",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_empty_role_blocked",
			enabled:    "true",
			role:       stringPtr(""),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_allows_authenticated_auth_me_self_read",
			enabled:    "true",
			role:       stringPtr("user"),
			subject:    &AuthSubject{UserID: 123},
			path:       "/api/v1/auth/me",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_authenticated_ide_usage_self_read",
			enabled:    "true",
			role:       stringPtr("user"),
			subject:    &AuthSubject{UserID: 123},
			path:       "/ide/api/usage",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_blocks_self_read_without_subject",
			enabled:    "true",
			role:       stringPtr("user"),
			path:       "/api/v1/auth/me",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_blocks_self_read_post",
			enabled:    "true",
			role:       stringPtr("user"),
			subject:    &AuthSubject{UserID: 123},
			method:     http.MethodPost,
			path:       "/api/v1/auth/me",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_blocks_other_ide_api",
			enabled:    "true",
			role:       stringPtr("user"),
			subject:    &AuthSubject{UserID: 123},
			path:       "/ide/api/models",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			r := gin.New()
			if tc.role != nil {
				role := *tc.role
				r.Use(func(c *gin.Context) {
					c.Set(string(ContextKeyUserRole), role)
					c.Next()
				})
			}
			if tc.subject != nil {
				subject := *tc.subject
				r.Use(func(c *gin.Context) {
					c.Set(string(ContextKeyUser), subject)
					c.Next()
				})
			}

			var svc *service.SettingService
			if !tc.nilService {
				svc = newBackendModeSettingService(t, tc.enabled)
			}

			r.Use(BackendModeUserGuard(svc))
			r.Any("/*path", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			w := httptest.NewRecorder()
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			path := tc.path
			if path == "" {
				path = "/test"
			}
			req := httptest.NewRequest(method, path, nil)
			r.ServeHTTP(w, req)

			require.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

func TestBackendModeAuthGuard(t *testing.T) {
	tests := []struct {
		name       string
		nilService bool
		enabled    string
		method     string
		path       string
		wantStatus int
	}{
		{
			name:       "disabled_allows_all",
			enabled:    "false",
			path:       "/api/v1/auth/register",
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil_service_allows_all",
			nilService: true,
			path:       "/api/v1/auth/register",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_login",
			enabled:    "true",
			path:       "/api/v1/auth/login",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_login_2fa",
			enabled:    "true",
			path:       "/api/v1/auth/login/2fa",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_logout",
			enabled:    "true",
			path:       "/api/v1/auth/logout",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_refresh",
			enabled:    "true",
			path:       "/api/v1/auth/refresh",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_ide_token_exchange",
			enabled:    "true",
			method:     http.MethodPost,
			path:       "/ide/auth/token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_blocks_ide_token_exchange_get",
			enabled:    "true",
			method:     http.MethodGet,
			path:       "/ide/auth/token",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_blocks_linuxdo_oauth_start",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/linuxdo/start",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_allows_linuxdo_oauth_callback",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/linuxdo/callback",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_blocks_wechat_oauth_start",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/wechat/start",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_allows_wechat_oauth_callback",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/wechat/callback",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_blocks_wechat_payment_oauth_start",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/wechat/payment/start",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_allows_wechat_payment_oauth_callback",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/wechat/payment/callback",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_blocks_oidc_oauth_start",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/oidc/start",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_allows_oidc_oauth_callback",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/oidc/callback",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_blocks_github_oauth_start",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/github/start",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_allows_github_oauth_callback",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/github/callback",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_blocks_google_oauth_start",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/google/start",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_allows_google_oauth_callback",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/google/callback",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_oauth_pending_exchange",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/pending/exchange",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_oauth_pending_send_verify_code",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/pending/send-verify-code",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_oauth_pending_create_account",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/pending/create-account",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_oauth_pending_bind_login",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/pending/bind-login",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_provider_bind_login",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/oidc/bind-login",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_provider_create_account",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/wechat/create-account",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_allows_legacy_complete_registration",
			enabled:    "true",
			path:       "/api/v1/auth/oauth/linuxdo/complete-registration",
			wantStatus: http.StatusOK,
		},
		{
			name:       "enabled_blocks_register",
			enabled:    "true",
			path:       "/api/v1/auth/register",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "enabled_blocks_forgot_password",
			enabled:    "true",
			path:       "/api/v1/auth/forgot-password",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)

			r := gin.New()

			var svc *service.SettingService
			if !tc.nilService {
				svc = newBackendModeSettingService(t, tc.enabled)
			}

			r.Use(BackendModeAuthGuard(svc))
			r.Any("/*path", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			w := httptest.NewRecorder()
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tc.path, nil)
			r.ServeHTTP(w, req)

			require.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLocalizeGatewayErrorMessage(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		code     string
		message  string
		expected string
	}{
		{
			name:     "model not found",
			status:   http.StatusNotFound,
			message:  "model is not found",
			expected: "模型不存在或暂不可用",
		},
		{
			name:     "service unavailable",
			status:   http.StatusServiceUnavailable,
			message:  "Service temporarily unavailable",
			expected: "服务暂时不可用，请稍后重试",
		},
		{
			name:     "preserves chinese content gateway reason",
			status:   http.StatusForbidden,
			message:  "中国地区内容安全网关配置不可用，请联系管理员",
			expected: "中国地区内容安全网关配置不可用，请联系管理员",
		},
		{
			name:     "insufficient balance by code",
			status:   http.StatusForbidden,
			code:     "INSUFFICIENT_BALANCE",
			message:  "Insufficient account balance",
			expected: "账户余额不足，请充值后重试",
		},
		{
			name:     "inactive user by code",
			status:   http.StatusUnauthorized,
			code:     "USER_INACTIVE",
			message:  "User account is not active",
			expected: "账号未激活，请完成账号激活后重试",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, LocalizeGatewayErrorMessage(tt.status, tt.code, tt.message))
		})
	}
}

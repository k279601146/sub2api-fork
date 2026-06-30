package service

import (
	"net/http"
	"strings"
)

func LocalizeGatewayErrorMessage(statusCode int, code, message string) string {
	trimmed := strings.TrimSpace(message)
	upperCode := strings.ToUpper(strings.TrimSpace(code))
	lowerMessage := strings.ToLower(trimmed)

	if containsCJK(trimmed) {
		return trimmed
	}

	switch upperCode {
	case "USER_INACTIVE", "USER_NOT_ACTIVE":
		return "账号未激活，请完成账号激活后重试"
	case "INSUFFICIENT_BALANCE":
		return "账户余额不足，请充值后重试"
	case "TOKEN_EXPIRED":
		return "登录已过期，请重新登录后重试"
	case "INVALID_TOKEN", "TOKEN_REVOKED", "UNAUTHORIZED":
		return "登录状态无效，请重新登录后重试"
	case "API_KEY_REQUIRED":
		return "请提供 API Key"
	case "INVALID_API_KEY":
		return "API Key 无效"
	case "API_KEY_DISABLED":
		return "API Key 已被禁用"
	case "API_KEY_QUOTA_EXHAUSTED", "USAGE_LIMIT_EXCEEDED":
		return "用量已达上限，请稍后重试"
	case "API_KEY_EXPIRED":
		return "API Key 已过期"
	case "SUBSCRIPTION_NOT_FOUND":
		return "当前分组没有可用订阅，请联系管理员"
	case "ACCESS_DENIED", "FORBIDDEN":
		return "访问被拒绝"
	}

	switch {
	case strings.Contains(lowerMessage, "user account is not active"):
		return "账号未激活，请完成账号激活后重试"
	case strings.Contains(lowerMessage, "insufficient account balance") ||
		strings.Contains(lowerMessage, "insufficient balance") ||
		strings.Contains(lowerMessage, "billing issue"):
		return "账户余额不足，请充值后重试"
	case strings.Contains(lowerMessage, "model is not found") ||
		(strings.Contains(lowerMessage, "model") && strings.Contains(lowerMessage, "not found")):
		return "模型不存在或暂不可用"
	case strings.Contains(lowerMessage, "service temporarily unavailable"):
		return "服务暂时不可用，请稍后重试"
	case strings.Contains(lowerMessage, "all available accounts exhausted"):
		return "当前没有可用模型通道，请稍后重试或联系管理员"
	case strings.Contains(lowerMessage, "upstream authentication failed"):
		return "API认证失败，请联系管理员"
	case strings.Contains(lowerMessage, "upstream access forbidden"):
		return "API访问被拒绝，请联系管理员"
	case strings.Contains(lowerMessage, "upstream rate limit exceeded") ||
		strings.Contains(lowerMessage, "rate limit"):
		return "请求频率已达上限，请稍后重试"
	case strings.Contains(lowerMessage, "upstream service overloaded"):
		return "API服务繁忙，请稍后重试"
	case strings.Contains(lowerMessage, "upstream request failed") ||
		strings.Contains(lowerMessage, "upstream gateway error"):
		return "API服务请求失败，请稍后重试"
	case strings.Contains(lowerMessage, "invalid api key"):
		return "API Key 无效"
	case strings.Contains(lowerMessage, "token has expired"):
		return "登录已过期，请重新登录后重试"
	case strings.Contains(lowerMessage, "invalid token") ||
		strings.Contains(lowerMessage, "token has been revoked"):
		return "登录状态无效，请重新登录后重试"
	case strings.Contains(lowerMessage, "user not found"):
		return "账号不存在，请重新登录后重试"
	}

	if trimmed != "" {
		return trimmed
	}

	switch {
	case statusCode == http.StatusUnauthorized:
		return "登录状态无效，请重新登录后重试"
	case statusCode == http.StatusForbidden:
		return "当前请求未被允许，请检查账号、模型权限或联系管理员"
	case statusCode == http.StatusNotFound:
		return "请求的资源不存在或模型暂不可用"
	case statusCode == http.StatusTooManyRequests:
		return "请求频率已达上限，请稍后重试"
	case statusCode >= http.StatusInternalServerError:
		return "服务暂时不可用，请稍后重试"
	default:
		return "请求失败"
	}
}

func containsCJK(message string) bool {
	for _, r := range message {
		if r >= '\u3400' && r <= '\u9fff' {
			return true
		}
	}
	return false
}

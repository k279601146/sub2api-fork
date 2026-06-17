package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultChinaGatewayDouyinBaseURL = "https://developer.toutiao.com"
	chinaGatewayDouyinTokenPath      = "/api/apps/v2/token"
	chinaGatewayDouyinTextPath       = "/api/v2/tags/text/antidirt"

	chinaGatewayPolicyVersion = "china_region_safety_gateway_v1"
	chinaGatewayAllowCacheTTL = 5 * time.Minute
	chinaGatewayBlockCacheTTL = 24 * time.Hour
)

type chinaGatewayCachedDecision struct {
	Allowed         bool
	Action          string
	HighestCategory string
	HighestScore    float64
	Message         string
	ExpiresAt       time.Time
}

type douyinTokenRequest struct {
	AppID     string `json:"appid"`
	Secret    string `json:"secret"`
	GrantType string `json:"grant_type"`
}

type douyinTextModerationRequest struct {
	Tasks []douyinTextModerationTask `json:"tasks"`
}

type douyinTextModerationTask struct {
	Content string `json:"content"`
}

type classifierRequest struct {
	Model          string              `json:"model"`
	Temperature    float64             `json:"temperature"`
	ResponseFormat *classifierJSONMode `json:"response_format,omitempty"`
	Messages       []classifierMessage `json:"messages"`
}

type classifierJSONMode struct {
	Type string `json:"type"`
}

type classifierMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type classifierResponse struct {
	Choices []struct {
		Message classifierMessage `json:"message"`
	} `json:"choices"`
}

type chinaClassifierDecision struct {
	Action     string  `json:"action"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func (s *ContentModerationService) checkChinaRegionSafetyGateway(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string) *ContentModerationDecision {
	if s == nil || cfg == nil {
		return nil
	}
	text := strings.TrimSpace(content.Text)
	if text == "" {
		return &ContentModerationDecision{Allowed: true, Action: ContentModerationActionAllow}
	}
	if s.hashCache != nil {
		matched, err := s.hashCache.HasFlaggedInputHash(ctx, hashText)
		if err != nil {
			slog.Warn("content_moderation.china_gateway.hash_check_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "error", err)
		}
		if matched {
			return s.chinaGatewayBlockDecision(ctx, input, cfg, content, hashText, ContentModerationActionHashBlock, "china_region/hash_hit", 1, nil, "", "")
		}
	}
	if cached, ok := s.getChinaGatewayCachedDecision(hashText); ok {
		if cached.Allowed {
			return &ContentModerationDecision{Allowed: true, Action: cached.Action, HighestCategory: cached.HighestCategory, HighestScore: cached.HighestScore}
		}
		return s.chinaGatewayBlockDecision(ctx, input, cfg, content, hashText, cached.Action, cached.HighestCategory, cached.HighestScore, nil, cached.Message, "")
	}
	if err := validateChinaGatewayConfig(cfg); err != nil {
		return s.chinaGatewayConfigErrorDecision(ctx, input, &content, err.Error())
	}

	start := time.Now()
	result, err := s.callDouyinTextModeration(ctx, cfg, text)
	douyinLatency := int(time.Since(start).Milliseconds())
	if err == nil {
		switch result {
		case ContentModerationActionDouyinBlock:
			decision := s.chinaGatewayBlockDecision(ctx, input, cfg, content, hashText, ContentModerationActionDouyinBlock, "china_region/douyin_hit", 1, &douyinLatency, "", "")
			s.setChinaGatewayCachedDecision(hashText, chinaGatewayCachedDecision{Allowed: false, Action: decision.Action, HighestCategory: decision.HighestCategory, HighestScore: decision.HighestScore, Message: decision.Message}, chinaGatewayBlockCacheTTL)
			return decision
		case ContentModerationActionDouyinPass:
			s.chinaGatewayAllowDecision(ctx, input, cfg, content, ContentModerationActionDouyinPass, "china_region/douyin_pass", 0, &douyinLatency)
			s.setChinaGatewayCachedDecision(hashText, chinaGatewayCachedDecision{Allowed: true, Action: ContentModerationActionDouyinPass, HighestCategory: "china_region/douyin_pass"}, chinaGatewayAllowCacheTTL)
			return &ContentModerationDecision{Allowed: true, Action: ContentModerationActionDouyinPass, HighestCategory: "china_region/douyin_pass"}
		}
	}
	if err != nil {
		slog.Warn("content_moderation.china_gateway.douyin_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "latency_ms", douyinLatency, "error", err)
	}

	classifierStart := time.Now()
	classifierDecision, classifierErr := s.callChinaGatewayClassifier(ctx, cfg, text)
	classifierLatency := int(time.Since(classifierStart).Milliseconds())
	if classifierErr != nil {
		slog.Warn("content_moderation.china_gateway.classifier_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "latency_ms", classifierLatency, "error", classifierErr)
		decision := s.chinaGatewayBlockDecision(ctx, input, cfg, content, hashText, ContentModerationActionClassifierErrorBlock, "china_region/classifier_error", 1, &classifierLatency, "", classifierErr.Error())
		s.setChinaGatewayCachedDecision(hashText, chinaGatewayCachedDecision{Allowed: false, Action: decision.Action, HighestCategory: decision.HighestCategory, HighestScore: decision.HighestScore, Message: decision.Message}, chinaGatewayBlockCacheTTL)
		return decision
	}
	category := strings.TrimSpace(classifierDecision.Category)
	if category == "" {
		category = "china_region/classifier"
	}
	score := classifierDecision.Confidence
	switch strings.ToLower(strings.TrimSpace(classifierDecision.Action)) {
	case "allow":
		s.chinaGatewayAllowDecision(ctx, input, cfg, content, ContentModerationActionClassifierAllow, category, score, &classifierLatency)
		s.setChinaGatewayCachedDecision(hashText, chinaGatewayCachedDecision{Allowed: true, Action: ContentModerationActionClassifierAllow, HighestCategory: category, HighestScore: score}, chinaGatewayAllowCacheTTL)
		return &ContentModerationDecision{Allowed: true, Action: ContentModerationActionClassifierAllow, HighestCategory: category, HighestScore: score}
	case "block":
		decision := s.chinaGatewayBlockDecision(ctx, input, cfg, content, hashText, ContentModerationActionClassifierBlock, category, score, &classifierLatency, "", classifierDecision.Reason)
		s.setChinaGatewayCachedDecision(hashText, chinaGatewayCachedDecision{Allowed: false, Action: decision.Action, HighestCategory: decision.HighestCategory, HighestScore: decision.HighestScore, Message: decision.Message}, chinaGatewayBlockCacheTTL)
		return decision
	default:
		errText := fmt.Sprintf("classifier returned invalid action %q", classifierDecision.Action)
		decision := s.chinaGatewayBlockDecision(ctx, input, cfg, content, hashText, ContentModerationActionClassifierErrorBlock, "china_region/classifier_invalid", 1, &classifierLatency, "", errText)
		s.setChinaGatewayCachedDecision(hashText, chinaGatewayCachedDecision{Allowed: false, Action: decision.Action, HighestCategory: decision.HighestCategory, HighestScore: decision.HighestScore, Message: decision.Message}, chinaGatewayBlockCacheTTL)
		return decision
	}
}

func validateChinaGatewayConfig(cfg *ContentModerationConfig) error {
	if cfg == nil {
		return errors.New("中国地区内容安全网关配置缺失")
	}
	if strings.TrimSpace(cfg.DouyinAppID) == "" {
		return errors.New("中国地区内容安全网关缺少 douyin_app_id")
	}
	if strings.TrimSpace(cfg.DouyinAppSecret) == "" {
		return errors.New("中国地区内容安全网关缺少 douyin_app_secret")
	}
	if strings.TrimSpace(cfg.ClassifierBaseURL) == "" {
		return errors.New("中国地区内容安全网关缺少 classifier_base_url")
	}
	if strings.TrimSpace(cfg.ClassifierAPIKey) == "" {
		return errors.New("中国地区内容安全网关缺少 classifier_api_key")
	}
	if strings.TrimSpace(cfg.ClassifierModel) == "" {
		return errors.New("中国地区内容安全网关缺少 classifier_model")
	}
	return nil
}

func (s *ContentModerationService) callDouyinTextModeration(ctx context.Context, cfg *ContentModerationConfig, text string) (string, error) {
	token, err := s.douyinAccessToken(ctx, cfg)
	if err != nil {
		return "", err
	}
	endpoint, err := url.JoinPath(strings.TrimRight(cfg.DouyinBaseURL, "/"), chinaGatewayDouyinTextPath)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(douyinTextModerationRequest{Tasks: []douyinTextModerationTask{{Content: text}}})
	if err != nil {
		return "", err
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.DouyinTimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", token)

	resp, err := s.httpClientOrDefault().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("douyin text moderation status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	result, err := parseDouyinTextModerationResult(body)
	if err != nil {
		return "", err
	}
	return result, nil
}

func (s *ContentModerationService) douyinAccessToken(ctx context.Context, cfg *ContentModerationConfig) (string, error) {
	now := time.Now()
	s.chinaGatewayMu.Lock()
	if s.chinaGatewayToken != "" && s.chinaGatewayTokenExpiry.After(now.Add(time.Minute)) {
		token := s.chinaGatewayToken
		s.chinaGatewayMu.Unlock()
		return token, nil
	}
	s.chinaGatewayMu.Unlock()

	endpoint, err := url.JoinPath(strings.TrimRight(cfg.DouyinBaseURL, "/"), chinaGatewayDouyinTokenPath)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(douyinTokenRequest{
		AppID:     cfg.DouyinAppID,
		Secret:    cfg.DouyinAppSecret,
		GrantType: "client_credential",
	})
	if err != nil {
		return "", err
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.DouyinTimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClientOrDefault().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("douyin token status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	token, ttl, err := parseDouyinAccessToken(body)
	if err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = 7200
	}
	s.chinaGatewayMu.Lock()
	s.chinaGatewayToken = token
	s.chinaGatewayTokenExpiry = now.Add(time.Duration(ttl) * time.Second)
	s.chinaGatewayMu.Unlock()
	return token, nil
}

func parseDouyinAccessToken(body []byte) (string, int64, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", 0, err
	}
	token, _ := findStringValue(payload, "access_token")
	if token == "" {
		token, _ = findStringValue(payload, "token")
	}
	if token == "" {
		return "", 0, errors.New("douyin token response missing access_token")
	}
	ttl, _ := findNumberValue(payload, "expires_in")
	return token, int64(ttl), nil
}

func parseDouyinTextModerationResult(body []byte) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if errNo, ok := findNumberValue(payload, "err_no"); ok && errNo != 0 {
		errMsg, _ := findStringValue(payload, "err_msg")
		if errMsg == "" {
			errMsg, _ = findStringValue(payload, "err_tips")
		}
		return "", fmt.Errorf("douyin moderation err_no %.0f: %s", errNo, errMsg)
	}
	seenHit, blocked := findDouyinHit(payload)
	if !seenHit {
		return "", errors.New("douyin moderation response missing hit result")
	}
	if blocked {
		return ContentModerationActionDouyinBlock, nil
	}
	if containsModerationReview(payload) {
		return "", errors.New("douyin moderation returned review-like result")
	}
	return ContentModerationActionDouyinPass, nil
}

func findDouyinHit(value any) (bool, bool) {
	switch v := value.(type) {
	case map[string]any:
		seen := false
		blocked := false
		if raw, ok := v["hit"]; ok {
			if hit, ok := raw.(bool); ok {
				seen = true
				blocked = hit
			}
		}
		for _, item := range v {
			childSeen, childBlocked := findDouyinHit(item)
			seen = seen || childSeen
			blocked = blocked || childBlocked
		}
		return seen, blocked
	case []any:
		seen := false
		blocked := false
		for _, item := range v {
			childSeen, childBlocked := findDouyinHit(item)
			seen = seen || childSeen
			blocked = blocked || childBlocked
		}
		return seen, blocked
	default:
		return false, false
	}
}

func containsModerationReview(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for _, item := range v {
			if containsModerationReview(item) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if containsModerationReview(item) {
				return true
			}
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "review", "manual_review", "manual", "audit", "need_review", "needs_review":
			return true
		}
	}
	return false
}

func (s *ContentModerationService) callChinaGatewayClassifier(ctx context.Context, cfg *ContentModerationConfig, text string) (*chinaClassifierDecision, error) {
	endpoint, err := classifierChatCompletionsEndpoint(cfg.ClassifierBaseURL)
	if err != nil {
		return nil, err
	}
	payload := classifierRequest{
		Model:       cfg.ClassifierModel,
		Temperature: 0,
		ResponseFormat: &classifierJSONMode{
			Type: "json_object",
		},
		Messages: []classifierMessage{
			{Role: "system", Content: chinaGatewayClassifierSystemPrompt()},
			{Role: "user", Content: text},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ClassifierTimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.ClassifierAPIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClientOrDefault().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("classifier status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out classifierResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, errors.New("classifier response missing choices")
	}
	return parseChinaClassifierDecision(out.Choices[0].Message.Content)
}

func classifierChatCompletionsEndpoint(baseURL string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(base, "/v1") {
		return url.JoinPath(base, "/chat/completions")
	}
	return url.JoinPath(base, "/v1/chat/completions")
}

func chinaGatewayClassifierSystemPrompt() string {
	return `你是中国地区 Safety Policy Gateway 的结构化分类器。只输出 JSON，不输出解释。
根据用户最后一条真实请求判断是否违反中国地区内容安全边界或明显违法违规边界。
输出格式必须为：{"action":"allow|block","category":"china_region_policy|illegal_instruction|privacy_abuse|cyber_abuse|fraud|sexual|violence|self_harm|other","confidence":0.0,"reason":"简短原因"}。
若请求涉及中国政治、中国党政机关、政治人物、国家统一、主权、领土完整、国家安全、分裂、颠覆、敏感历史恶意传播，或涉及诈骗、赌博、色情、毒品、暴力、恐怖极端、网络入侵、密码破解、伪造证件、身份盗用、隐私窃取、账号接管、绕过实名/风控/审核等，action 必须为 block。
普通软件工程、文档处理、数据处理、产品设计、合规安全系统设计可 allow。`
}

func parseChinaClassifierDecision(content string) (*chinaClassifierDecision, error) {
	raw := strings.TrimSpace(content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var decision chinaClassifierDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return nil, err
	}
	decision.Action = strings.ToLower(strings.TrimSpace(decision.Action))
	decision.Category = strings.TrimSpace(decision.Category)
	decision.Reason = strings.TrimSpace(decision.Reason)
	if decision.Confidence < 0 {
		decision.Confidence = 0
	}
	if decision.Confidence > 1 {
		decision.Confidence = 1
	}
	return &decision, nil
}

func (s *ContentModerationService) chinaGatewayConfigErrorDecision(ctx context.Context, input ContentModerationCheckInput, content *ContentModerationInput, errText string) *ContentModerationDecision {
	cfg := defaultContentModerationConfig()
	text := ""
	hashText := ""
	if content != nil {
		text = content.ExcerptText()
		hashText = content.Hash()
	}
	return s.chinaGatewayBlockDecision(ctx, input, cfg, ContentModerationInput{Text: text}, hashText, ContentModerationActionChinaConfigErrorBlock, "china_region/config_error", 1, nil, "中国地区内容安全网关配置不可用，请联系管理员", errText)
}

func (s *ContentModerationService) chinaGatewayBlockDecision(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, hashText string, action string, category string, score float64, latency *int, message string, errText string) *ContentModerationDecision {
	if cfg == nil {
		cfg = defaultContentModerationConfig()
	}
	if strings.TrimSpace(message) == "" {
		message = chinaGatewayBlockMessage(cfg)
	}
	scores := map[string]float64{category: score}
	log := s.buildLog(input, cfg, action, true, category, score, scores, content.ExcerptText(), latency, nil, errText)
	if hashText != "" && s.hashCache != nil {
		if err := s.hashCache.RecordFlaggedInputHash(ctx, hashText); err != nil {
			slog.Warn("content_moderation.china_gateway.record_hash_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "error", err)
		}
	}
	countViolation := s.shouldCountChinaGatewayViolation(input.UserID, hashText, cfg.ViolationWindowHours)
	if countViolation {
		s.applyFlaggedSideEffects(ctx, cfg, log)
	}
	if !countViolation {
		log.ViolationCount = 0
	}
	if s.repo != nil {
		if err := s.repo.CreateLog(ctx, log); err != nil {
			slog.Warn("content_moderation.china_gateway.block_log_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "action", action, "error", err)
		}
	}
	return &ContentModerationDecision{
		Allowed:         false,
		Blocked:         true,
		Flagged:         true,
		Message:         message,
		StatusCode:      cfg.BlockStatus,
		InputHash:       hashText,
		HighestCategory: category,
		HighestScore:    score,
		CategoryScores:  scores,
		Action:          action,
	}
}

func (s *ContentModerationService) chinaGatewayAllowDecision(ctx context.Context, input ContentModerationCheckInput, cfg *ContentModerationConfig, content ContentModerationInput, action string, category string, score float64, latency *int) {
	if s == nil || cfg == nil || !cfg.RecordNonHits || s.repo == nil {
		return
	}
	scores := map[string]float64{category: score}
	log := s.buildLog(input, cfg, action, false, category, score, scores, content.ExcerptText(), latency, nil, "")
	if err := s.repo.CreateLog(ctx, log); err != nil {
		slog.Warn("content_moderation.china_gateway.allow_log_failed", "user_id", input.UserID, "endpoint", input.Endpoint, "action", action, "error", err)
	}
}

func chinaGatewayBlockMessage(cfg *ContentModerationConfig) string {
	if cfg != nil && strings.TrimSpace(cfg.BlockMessage) != "" {
		return strings.TrimSpace(cfg.BlockMessage)
	}
	return "中国地区内容安全网关命中风险规则，请调整输入后重试"
}

func (s *ContentModerationService) getChinaGatewayCachedDecision(inputHash string) (chinaGatewayCachedDecision, bool) {
	key := chinaGatewayCacheKey(inputHash)
	if key == "" || s == nil {
		return chinaGatewayCachedDecision{}, false
	}
	now := time.Now()
	s.chinaGatewayMu.Lock()
	defer s.chinaGatewayMu.Unlock()
	if s.chinaGatewayCache == nil {
		return chinaGatewayCachedDecision{}, false
	}
	decision, ok := s.chinaGatewayCache[key]
	if !ok {
		return chinaGatewayCachedDecision{}, false
	}
	if !decision.ExpiresAt.After(now) {
		delete(s.chinaGatewayCache, key)
		return chinaGatewayCachedDecision{}, false
	}
	return decision, true
}

func (s *ContentModerationService) setChinaGatewayCachedDecision(inputHash string, decision chinaGatewayCachedDecision, ttl time.Duration) {
	key := chinaGatewayCacheKey(inputHash)
	if key == "" || s == nil || ttl <= 0 {
		return
	}
	decision.ExpiresAt = time.Now().Add(ttl)
	s.chinaGatewayMu.Lock()
	defer s.chinaGatewayMu.Unlock()
	if s.chinaGatewayCache == nil {
		s.chinaGatewayCache = make(map[string]chinaGatewayCachedDecision)
	}
	s.chinaGatewayCache[key] = decision
}

func chinaGatewayCacheKey(inputHash string) string {
	inputHash = strings.TrimSpace(inputHash)
	if inputHash == "" {
		return ""
	}
	h := sha256.Sum256([]byte(chinaGatewayPolicyVersion + ":" + inputHash))
	return hex.EncodeToString(h[:])
}

func shouldApplyChinaGateway(provider string, model string) bool {
	return !isChinaDomesticAIModel(provider, model)
}

func isChinaDomesticAIModel(provider string, model string) bool {
	text := strings.ToLower(strings.TrimSpace(provider + " " + model))
	if text == "" {
		return false
	}
	domesticMarkers := []string{
		"qwen", "tongyi", "aliyun", "dashscope", "baichuan", "ernie", "wenxin", "baidu",
		"hunyuan", "tencent", "yuanbao", "doubao", "volcengine", "bytedance", "ark",
		"moonshot", "kimi", "minimax", "abab", "glm", "chatglm", "zhipu", "bigmodel",
		"spark", "xinghuo", "iflytek", "deepseek", "yi-", "01-ai", "stepfun", "step-",
		"sensenova", "sensechat", "siliconflow", "baai", "aquila",
	}
	for _, marker := range domesticMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func (s *ContentModerationService) shouldCountChinaGatewayViolation(userID int64, inputHash string, windowHours int) bool {
	if s == nil || userID <= 0 || strings.TrimSpace(inputHash) == "" {
		return true
	}
	if windowHours <= 0 {
		windowHours = defaultChinaGatewayViolationWindowHours
	}
	key := fmt.Sprintf("%d:%s", userID, chinaGatewayCacheKey(inputHash))
	if strings.TrimSpace(key) == "" {
		return true
	}
	now := time.Now()
	expiresAt := now.Add(time.Duration(windowHours) * time.Hour)
	s.chinaGatewayMu.Lock()
	defer s.chinaGatewayMu.Unlock()
	if s.chinaGatewayViolationTTL == nil {
		s.chinaGatewayViolationTTL = make(map[string]time.Time)
	}
	if existing, ok := s.chinaGatewayViolationTTL[key]; ok && existing.After(now) {
		return false
	}
	s.chinaGatewayViolationTTL[key] = expiresAt
	return true
}

func (s *ContentModerationService) httpClientOrDefault() *http.Client {
	if s != nil && s.httpClient != nil {
		return s.httpClient
	}
	return http.DefaultClient
}

func newContentModerationHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxyFromEnvironmentSkippingLoopback(http.ProxyFromEnvironment)
	return &http.Client{Transport: transport}
}

func proxyFromEnvironmentSkippingLoopback(base func(*http.Request) (*url.URL, error)) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if req != nil && req.URL != nil && shouldBypassProxyForHost(req.URL.Hostname()) {
			return nil, nil
		}
		return base(req)
	}
}

func shouldBypassProxyForHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return false
	}
	if host == "localhost" || host == "localhost." || host == "::1" {
		return true
	}
	if strings.HasPrefix(host, "127.") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func findStringValue(value any, key string) (string, bool) {
	switch v := value.(type) {
	case map[string]any:
		if raw, ok := v[key]; ok {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s), true
			}
		}
		for _, item := range v {
			if s, ok := findStringValue(item, key); ok {
				return s, true
			}
		}
	case []any:
		for _, item := range v {
			if s, ok := findStringValue(item, key); ok {
				return s, true
			}
		}
	}
	return "", false
}

func findNumberValue(value any, key string) (float64, bool) {
	switch v := value.(type) {
	case map[string]any:
		if raw, ok := v[key]; ok {
			switch n := raw.(type) {
			case float64:
				return n, true
			case int:
				return float64(n), true
			case int64:
				return float64(n), true
			}
		}
		for _, item := range v {
			if n, ok := findNumberValue(item, key); ok {
				return n, true
			}
		}
	case []any:
		for _, item := range v {
			if n, ok := findNumberValue(item, key); ok {
				return n, true
			}
		}
	}
	return 0, false
}

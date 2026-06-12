package service

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

func (s *OpenAIGatewayService) forwardResponsesAsChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	responsesBody []byte,
	token string,
	originalModel string,
	billingModel string,
	upstreamModel string,
	clientStream bool,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	chatBody, err := responsesBodyToChatCompletionsBody(responsesBody, upstreamModel)
	if err != nil {
		writeResponsesError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}
	if clientStream {
		chatBody, err = ensureOpenAIChatStreamUsage(chatBody)
		if err != nil {
			return nil, fmt.Errorf("enable stream usage: %w", err)
		}
	}
	setOpsUpstreamRequestBody(c, chatBody)

	baseURL := account.GetOpenAIBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}
	validatedURL, err := s.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	targetURL := buildOpenAIChatCompletionsURL(validatedURL)

	upstreamCtx, releaseUpstreamCtx := detachUpstreamContext(ctx)
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(chatBody))
	releaseUpstreamCtx()
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Authorization", "Bearer "+token)
	if clientStream {
		upstreamReq.Header.Set("Accept", "text/event-stream")
	} else {
		upstreamReq.Header.Set("Accept", "application/json")
	}
	for key, values := range c.Request.Header {
		if openaiCCRawAllowedHeaders[strings.ToLower(key)] {
			for _, v := range values {
				upstreamReq.Header.Add(key, v)
			}
		}
	}
	if customUA := strings.TrimSpace(account.GetOpenAIUserAgent()); customUA != "" {
		upstreamReq.Header.Set("User-Agent", customUA)
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	upstreamStart := time.Now()
	resp, err := s.httpUpstream.Do(upstreamReq, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(upstreamStart).Milliseconds())
	if err != nil {
		safeErr := sanitizeUpstreamErrorMessage(err.Error())
		setOpsUpstreamError(c, 0, safeErr, "")
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: 0,
			Kind:               "request_error",
			Message:            safeErr,
		})
		writeResponsesError(c, http.StatusBadGateway, "server_error", "Upstream request failed")
		return nil, fmt.Errorf("upstream request failed: %s", safeErr)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(respBody))

		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
		if s.shouldFailoverOpenAIUpstreamResponse(resp.StatusCode, upstreamMsg, respBody) {
			if s.rateLimitService != nil {
				s.rateLimitService.HandleUpstreamError(ctx, account, resp.StatusCode, resp.Header, respBody)
			}
			return nil, &UpstreamFailoverError{
				StatusCode:             resp.StatusCode,
				ResponseBody:           respBody,
				RetryableOnSameAccount: account.IsPoolMode() && (isPoolModeRetryableStatus(resp.StatusCode) || isOpenAITransientProcessingError(resp.StatusCode, upstreamMsg, respBody)),
			}
		}
		statusCode := mapUpstreamStatusCode(resp.StatusCode)
		writeResponsesError(c, statusCode, "server_error", upstreamMsg)
		return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
	}

	var result *OpenAIForwardResult
	if clientStream {
		result, err = s.streamChatCompletionsAsResponses(c, resp, originalModel, billingModel, upstreamModel, startTime)
	} else {
		result, err = s.bufferChatCompletionsAsResponses(c, resp, originalModel, billingModel, upstreamModel, startTime)
	}
	if result != nil {
		result.ReasoningEffort = extractOpenAIReasoningEffortFromBody(responsesBody, originalModel)
		result.ServiceTier = extractOpenAIServiceTierFromBody(responsesBody)
	}
	return result, err
}

func responsesBodyToChatCompletionsBody(body []byte, upstreamModel string) ([]byte, error) {
	var req apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	if strings.TrimSpace(upstreamModel) != "" {
		req.Model = upstreamModel
	}
	messages, err := responsesInputToChatMessages(req.Instructions, req.Input)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		raw, _ := json.Marshal("hi")
		messages = append(messages, apicompat.ChatMessage{Role: "user", Content: raw})
	}

	out := apicompat.ChatCompletionsRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stream:      req.Stream,
		ServiceTier: req.ServiceTier,
	}
	if req.ParallelToolCalls != nil {
		out.ParallelToolCalls = req.ParallelToolCalls
	}
	if req.MaxOutputTokens != nil {
		out.MaxCompletionTokens = req.MaxOutputTokens
	}
	if req.Reasoning != nil {
		out.ReasoningEffort = req.Reasoning.Effort
	}
	for _, tool := range req.Tools {
		if tool.Type != "function" {
			continue
		}
		fn := &apicompat.ChatFunction{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.Parameters,
			Strict:      tool.Strict,
		}
		out.Tools = append(out.Tools, apicompat.ChatTool{Type: "function", Function: fn})
	}
	out.ToolChoice = responsesToolChoiceToChat(req.ToolChoice)
	return json.Marshal(out)
}

func responsesInputToChatMessages(instructions string, input json.RawMessage) ([]apicompat.ChatMessage, error) {
	var messages []apicompat.ChatMessage
	if strings.TrimSpace(instructions) != "" {
		raw, _ := json.Marshal(instructions)
		messages = append(messages, apicompat.ChatMessage{Role: "system", Content: raw})
	}
	if len(input) == 0 || bytes.Equal(bytes.TrimSpace(input), []byte("null")) {
		return messages, nil
	}
	var text string
	if err := json.Unmarshal(input, &text); err == nil {
		raw, _ := json.Marshal(text)
		return append(messages, apicompat.ChatMessage{Role: "user", Content: raw}), nil
	}
	var items []apicompat.ResponsesInputItem
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, fmt.Errorf("parse responses input: %w", err)
	}
	for _, item := range items {
		msgs, err := responsesInputItemToChatMessages(item)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msgs...)
	}
	return messages, nil
}

func responsesInputItemToChatMessages(item apicompat.ResponsesInputItem) ([]apicompat.ChatMessage, error) {
	switch item.Type {
	case "function_call":
		args := item.Arguments
		if args == "" {
			args = "{}"
		}
		return []apicompat.ChatMessage{{
			Role: "assistant",
			ToolCalls: []apicompat.ChatToolCall{{
				ID:   item.CallID,
				Type: "function",
				Function: apicompat.ChatFunctionCall{
					Name:      item.Name,
					Arguments: args,
				},
			}},
		}}, nil
	case "function_call_output":
		raw, _ := json.Marshal(item.Output)
		return []apicompat.ChatMessage{{Role: "tool", Content: raw, ToolCallID: item.CallID}}, nil
	}

	role := item.Role
	if role == "" {
		role = "user"
	}
	if role == "developer" {
		role = "system"
	}
	content, err := responsesContentToChatContent(item.Content, role == "assistant")
	if err != nil {
		return nil, err
	}
	return []apicompat.ChatMessage{{Role: role, Content: content}}, nil
}

func responsesContentToChatContent(raw json.RawMessage, assistant bool) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.Marshal("")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return json.Marshal(text)
	}
	var parts []apicompat.ResponsesContentPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("parse responses content: %w", err)
	}
	if assistant {
		var b strings.Builder
		for _, part := range parts {
			if part.Text != "" {
				b.WriteString(part.Text)
			}
		}
		return json.Marshal(b.String())
	}
	chatParts := make([]apicompat.ChatContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "input_text", "output_text":
			chatParts = append(chatParts, apicompat.ChatContentPart{Type: "text", Text: part.Text})
		case "input_image":
			chatParts = append(chatParts, apicompat.ChatContentPart{
				Type:     "image_url",
				ImageURL: &apicompat.ChatImageURL{URL: part.ImageURL},
			})
		}
	}
	return json.Marshal(chatParts)
}

func responsesToolChoiceToChat(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw
	}
	if obj["type"] == "function" {
		if name, _ := obj["name"].(string); name != "" {
			converted, _ := json.Marshal(map[string]any{
				"type":     "function",
				"function": map[string]any{"name": name},
			})
			return converted
		}
	}
	return raw
}

type responsesChatToolCall struct {
	ItemID    string
	CallID    string
	Name      string
	Arguments string
}

type responsesChatToolCallState struct {
	responsesChatToolCall
	OutputIndex int
	Started     bool
	Args        strings.Builder
}

type responsesChatReasoningState struct {
	ItemID      string
	OutputIndex int
	Started     bool
	Text        strings.Builder
}

type indexedResponsesOutput struct {
	Index int
	Item  map[string]any
}

func (s *OpenAIGatewayService) streamChatCompletionsAsResponses(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := strings.TrimSpace(resp.Header.Get("x-request-id"))
	responseID := "resp_" + randomHexID(12)

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	write := func(payload any) error {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := c.Writer.WriteString("data: " + string(b) + "\n\n"); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}

	_ = write(map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":     responseID,
			"object": "response",
			"model":  originalModel,
			"status": "in_progress",
			"output": []any{},
		},
	})

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := defaultMaxLineSize
	if s.cfg != nil && s.cfg.Gateway.MaxLineSize > 0 {
		maxLineSize = s.cfg.Gateway.MaxLineSize
	}
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var usage OpenAIUsage
	var outputText strings.Builder
	var firstTokenMs *int
	clientDisconnected := false
	finishReason := "stop"
	completed := false
	messageID := "msg_" + randomHexID(8)
	outputItemStarted := false
	messageOutputIndex := -1
	nextOutputIndex := 0
	toolCalls := map[int]*responsesChatToolCallState{}
	toolCallOrder := make([]int, 0)
	reasoning := responsesChatReasoningState{
		ItemID:      "rs_" + randomHexID(8),
		OutputIndex: -1,
	}

	writeIfConnected := func(payload any) {
		if clientDisconnected {
			return
		}
		if err := write(payload); err != nil {
			clientDisconnected = true
			logger.L().Debug("openai responses->chat stream: client disconnected, continuing to drain upstream", zap.Error(err), zap.String("request_id", requestID))
		}
	}
	ensureOutputItemStarted := func() {
		if outputItemStarted || clientDisconnected {
			return
		}
		outputItemStarted = true
		messageOutputIndex = nextOutputIndex
		nextOutputIndex++
		item := map[string]any{
			"type":    "message",
			"id":      messageID,
			"role":    "assistant",
			"status":  "in_progress",
			"content": []any{},
		}
		writeIfConnected(map[string]any{
			"type":         "response.output_item.added",
			"output_index": messageOutputIndex,
			"item":         item,
		})
		writeIfConnected(map[string]any{
			"type":          "response.content_part.added",
			"item_id":       messageID,
			"output_index":  messageOutputIndex,
			"content_index": 0,
			"part": map[string]any{
				"type": "output_text",
				"text": "",
			},
		})
	}
	ensureReasoningStarted := func() {
		if reasoning.Started || clientDisconnected {
			return
		}
		reasoning.Started = true
		reasoning.OutputIndex = nextOutputIndex
		nextOutputIndex++
		writeIfConnected(map[string]any{
			"type":         "response.output_item.added",
			"output_index": reasoning.OutputIndex,
			"item": map[string]any{
				"type":    "reasoning",
				"id":      reasoning.ItemID,
				"status":  "in_progress",
				"summary": []any{},
			},
		})
	}
	getToolCallState := func(index int) *responsesChatToolCallState {
		if state, ok := toolCalls[index]; ok {
			return state
		}
		state := &responsesChatToolCallState{
			responsesChatToolCall: responsesChatToolCall{
				ItemID: "fc_" + randomHexID(8),
				CallID: "call_" + randomHexID(8),
			},
			OutputIndex: nextOutputIndex,
		}
		nextOutputIndex++
		toolCalls[index] = state
		toolCallOrder = append(toolCallOrder, index)
		return state
	}
	ensureToolCallStarted := func(state *responsesChatToolCallState) {
		if state == nil || state.Started || clientDisconnected {
			return
		}
		state.Started = true
		writeIfConnected(map[string]any{
			"type":         "response.output_item.added",
			"output_index": state.OutputIndex,
			"item": map[string]any{
				"type":      "function_call",
				"id":        state.ItemID,
				"call_id":   state.CallID,
				"name":      state.Name,
				"arguments": "",
				"status":    "in_progress",
			},
		})
	}
	for scanner.Scan() {
		line := scanner.Text()
		payload, ok := extractOpenAISSEDataLine(line)
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			completed = true
			break
		}
		if u := extractCCStreamUsage(payload); u != nil {
			usage = *u
		}
		for _, choice := range gjson.Get(payload, "choices").Array() {
			reasoningDelta := firstNonEmptyGJSON(choice, "delta.reasoning_content", "delta.reasoning")
			if reasoningDelta != "" {
				reasoning.Text.WriteString(reasoningDelta)
				if firstTokenMs == nil {
					ms := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &ms
				}
				ensureReasoningStarted()
				writeIfConnected(map[string]any{
					"type":          "response.reasoning_summary_text.delta",
					"item_id":       reasoning.ItemID,
					"output_index":  reasoning.OutputIndex,
					"summary_index": 0,
					"delta":         reasoningDelta,
				})
			}
			content := choice.Get("delta.content").String()
			if content != "" {
				outputText.WriteString(content)
				if firstTokenMs == nil {
					ms := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &ms
				}
				ensureOutputItemStarted()
				writeIfConnected(map[string]any{
					"type":          "response.output_text.delta",
					"item_id":       messageID,
					"output_index":  messageOutputIndex,
					"content_index": 0,
					"delta":         content,
				})
			}
			for ordinal, toolCall := range choice.Get("delta.tool_calls").Array() {
				toolIndex := ordinal
				if toolCall.Get("index").Exists() {
					toolIndex = int(toolCall.Get("index").Int())
				}
				state := getToolCallState(toolIndex)
				if id := toolCall.Get("id").String(); id != "" && !state.Started {
					state.CallID = id
				}
				if nameDelta := toolCall.Get("function.name").String(); nameDelta != "" {
					state.Name += nameDelta
				}
				if state.Name != "" {
					ensureToolCallStarted(state)
				}
				if argsDelta := toolCall.Get("function.arguments").String(); argsDelta != "" {
					state.Args.WriteString(argsDelta)
					state.Arguments = state.Args.String()
					ensureToolCallStarted(state)
					writeIfConnected(map[string]any{
						"type":         "response.function_call_arguments.delta",
						"item_id":      state.ItemID,
						"output_index": state.OutputIndex,
						"call_id":      state.CallID,
						"name":         state.Name,
						"delta":        argsDelta,
					})
				}
			}
			legacyFunctionCall := choice.Get("delta.function_call")
			if legacyFunctionCall.Exists() {
				state := getToolCallState(0)
				if nameDelta := legacyFunctionCall.Get("name").String(); nameDelta != "" {
					state.Name += nameDelta
				}
				if state.Name != "" {
					ensureToolCallStarted(state)
				}
				if argsDelta := legacyFunctionCall.Get("arguments").String(); argsDelta != "" {
					state.Args.WriteString(argsDelta)
					state.Arguments = state.Args.String()
					ensureToolCallStarted(state)
					writeIfConnected(map[string]any{
						"type":         "response.function_call_arguments.delta",
						"item_id":      state.ItemID,
						"output_index": state.OutputIndex,
						"call_id":      state.CallID,
						"name":         state.Name,
						"delta":        argsDelta,
					})
				}
			}
			if reason := choice.Get("finish_reason").String(); reason != "" {
				finishReason = reason
			}
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return &OpenAIForwardResult{RequestID: requestID, Usage: usage, Model: originalModel, BillingModel: billingModel, UpstreamModel: upstreamModel, Stream: true, Duration: time.Since(startTime), FirstTokenMs: firstTokenMs}, err
	}

	status := "completed"
	var incompleteDetails map[string]any
	if finishReason == "length" {
		status = "incomplete"
		incompleteDetails = map[string]any{"reason": "max_output_tokens"}
	} else if finishReason == "content_filter" {
		status = "incomplete"
		incompleteDetails = map[string]any{"reason": "content_filter"}
	}
	reasoningText := reasoning.Text.String()
	outputs := make([]indexedResponsesOutput, 0, 2+len(toolCallOrder))
	if reasoning.Started {
		outputs = append(outputs, indexedResponsesOutput{
			Index: reasoning.OutputIndex,
			Item:  chatReasoningOutputItem(reasoning.ItemID, status, reasoningText),
		})
	}
	if outputItemStarted {
		outputs = append(outputs, indexedResponsesOutput{
			Index: messageOutputIndex,
			Item:  chatTextOutputItem(messageID, status, outputText.String()),
		})
	}
	for _, index := range toolCallOrder {
		state := toolCalls[index]
		if state == nil {
			continue
		}
		state.Arguments = state.Args.String()
		outputs = append(outputs, indexedResponsesOutput{
			Index: state.OutputIndex,
			Item:  chatToolCallOutputItem(state.responsesChatToolCall, status),
		})
	}
	response := chatResponseWithOutput(responseID, originalModel, status, sortedResponsesOutputItems(outputs), usage, incompleteDetails)
	if !clientDisconnected {
		if reasoning.Started {
			_ = write(map[string]any{
				"type":          "response.reasoning_summary_text.done",
				"item_id":       reasoning.ItemID,
				"output_index":  reasoning.OutputIndex,
				"summary_index": 0,
				"text":          reasoningText,
			})
			_ = write(map[string]any{
				"type":         "response.output_item.done",
				"output_index": reasoning.OutputIndex,
				"item":         chatReasoningOutputItem(reasoning.ItemID, status, reasoningText),
			})
		}
		if outputItemStarted {
			text := outputText.String()
			_ = write(map[string]any{
				"type":          "response.output_text.done",
				"item_id":       messageID,
				"output_index":  messageOutputIndex,
				"content_index": 0,
				"text":          text,
			})
			_ = write(map[string]any{
				"type":          "response.content_part.done",
				"item_id":       messageID,
				"output_index":  messageOutputIndex,
				"content_index": 0,
				"part": map[string]any{
					"type": "output_text",
					"text": text,
				},
			})
			_ = write(map[string]any{
				"type":         "response.output_item.done",
				"output_index": messageOutputIndex,
				"item":         chatTextOutputItem(messageID, status, text),
			})
		}
		for _, index := range toolCallOrder {
			state := toolCalls[index]
			if state == nil {
				continue
			}
			ensureToolCallStarted(state)
			arguments := state.Args.String()
			state.Arguments = arguments
			_ = write(map[string]any{
				"type":         "response.function_call_arguments.done",
				"item_id":      state.ItemID,
				"output_index": state.OutputIndex,
				"call_id":      state.CallID,
				"name":         state.Name,
				"arguments":    arguments,
			})
			_ = write(map[string]any{
				"type":         "response.output_item.done",
				"output_index": state.OutputIndex,
				"item":         chatToolCallOutputItem(state.responsesChatToolCall, status),
			})
		}
		_ = write(map[string]any{"type": "response.completed", "response": response})
		_, _ = c.Writer.WriteString("data: [DONE]\n\n")
		c.Writer.Flush()
	}
	_ = completed

	return &OpenAIForwardResult{
		RequestID:     requestID,
		Usage:         usage,
		Model:         originalModel,
		BillingModel:  billingModel,
		UpstreamModel: upstreamModel,
		Stream:        true,
		Duration:      time.Since(startTime),
		FirstTokenMs:  firstTokenMs,
	}, nil
}

func (s *OpenAIGatewayService) bufferChatCompletionsAsResponses(
	c *gin.Context,
	resp *http.Response,
	originalModel string,
	billingModel string,
	upstreamModel string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := strings.TrimSpace(resp.Header.Get("x-request-id"))
	body, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		if !errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
			writeResponsesError(c, http.StatusBadGateway, "server_error", "Failed to read upstream response")
		}
		return nil, err
	}

	var ccResp apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(body, &ccResp); err != nil {
		writeResponsesError(c, http.StatusBadGateway, "server_error", "Failed to parse upstream response")
		return nil, fmt.Errorf("parse chat completions response: %w", err)
	}
	response, usage := chatCompletionsResponseToResponses(&ccResp, originalModel)

	if s.responseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(c.Writer).Encode(response); err != nil {
		return nil, err
	}

	return &OpenAIForwardResult{
		RequestID:     requestID,
		Usage:         usage,
		Model:         originalModel,
		BillingModel:  billingModel,
		UpstreamModel: upstreamModel,
		Stream:        false,
		Duration:      time.Since(startTime),
	}, nil
}

func chatCompletionsResponseToResponses(resp *apicompat.ChatCompletionsResponse, model string) (map[string]any, OpenAIUsage) {
	usage := chatUsageToOpenAIUsage(resp.Usage)
	content := ""
	reasoning := ""
	var toolCalls []responsesChatToolCall
	finishReason := "stop"
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		finishReason = choice.FinishReason
		reasoning = choice.Message.ReasoningContent
		if reasoning == "" {
			reasoning = choice.Message.Reasoning
		}
		if len(choice.Message.Content) > 0 {
			_ = json.Unmarshal(choice.Message.Content, &content)
		}
		for _, toolCall := range choice.Message.ToolCalls {
			item := responsesChatToolCall{
				ItemID:    "fc_" + randomHexID(8),
				CallID:    toolCall.ID,
				Name:      toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
			}
			if item.CallID == "" {
				item.CallID = "call_" + randomHexID(8)
			}
			toolCalls = append(toolCalls, item)
		}
		if choice.Message.FunctionCall != nil {
			toolCalls = append(toolCalls, responsesChatToolCall{
				ItemID:    "fc_" + randomHexID(8),
				CallID:    "call_" + randomHexID(8),
				Name:      choice.Message.FunctionCall.Name,
				Arguments: choice.Message.FunctionCall.Arguments,
			})
		}
	}
	status := "completed"
	var incompleteDetails map[string]any
	if finishReason == "length" {
		status = "incomplete"
		incompleteDetails = map[string]any{"reason": "max_output_tokens"}
	} else if finishReason == "content_filter" {
		status = "incomplete"
		incompleteDetails = map[string]any{"reason": "content_filter"}
	}
	id := resp.ID
	if !strings.HasPrefix(id, "resp_") {
		id = "resp_" + strings.TrimPrefix(id, "chatcmpl_")
	}
	return chatOutputToResponsesResponse(id, model, status, reasoning, content, toolCalls, usage, incompleteDetails), usage
}

func chatTextToResponsesResponse(id, model, status, content string, usage OpenAIUsage, incompleteDetails map[string]any) map[string]any {
	return chatOutputToResponsesResponse(id, model, status, "", content, nil, usage, incompleteDetails)
}

func chatOutputToResponsesResponse(id, model, status, reasoning, content string, toolCalls []responsesChatToolCall, usage OpenAIUsage, incompleteDetails map[string]any) map[string]any {
	if id == "" {
		id = "resp_" + randomHexID(12)
	}
	output := make([]any, 0, 2+len(toolCalls))
	if reasoning != "" {
		output = append(output, chatReasoningOutputItem("rs_"+randomHexID(8), status, reasoning))
	}
	if content != "" || len(toolCalls) == 0 {
		output = append(output, chatTextOutputItem("msg_"+randomHexID(8), status, content))
	}
	for _, toolCall := range toolCalls {
		output = append(output, chatToolCallOutputItem(toolCall, status))
	}
	return chatResponseWithOutput(id, model, status, output, usage, incompleteDetails)
}

func chatResponseWithOutput(id, model, status string, output []any, usage OpenAIUsage, incompleteDetails map[string]any) map[string]any {
	if id == "" {
		id = "resp_" + randomHexID(12)
	}
	if len(output) == 0 {
		output = []any{chatTextOutputItem("msg_"+randomHexID(8), status, "")}
	}
	response := map[string]any{
		"id":     id,
		"object": "response",
		"model":  model,
		"status": status,
		"output": output,
		"usage": map[string]any{
			"input_tokens":  usage.InputTokens,
			"output_tokens": usage.OutputTokens,
			"total_tokens":  usage.InputTokens + usage.OutputTokens,
			"input_tokens_details": map[string]any{
				"cached_tokens": usage.CacheReadInputTokens,
			},
		},
	}
	if incompleteDetails != nil {
		response["incomplete_details"] = incompleteDetails
	}
	return response
}

func sortedResponsesOutputItems(outputs []indexedResponsesOutput) []any {
	sort.SliceStable(outputs, func(i, j int) bool {
		return outputs[i].Index < outputs[j].Index
	})
	items := make([]any, 0, len(outputs))
	for _, output := range outputs {
		items = append(items, output.Item)
	}
	return items
}

func chatReasoningOutputItem(id, status, reasoning string) map[string]any {
	if id == "" {
		id = "rs_" + randomHexID(8)
	}
	return map[string]any{
		"type":   "reasoning",
		"id":     id,
		"status": status,
		"summary": []any{
			map[string]any{
				"type": "summary_text",
				"text": reasoning,
			},
		},
	}
}

func chatTextOutputItem(id, status, content string) map[string]any {
	if id == "" {
		id = "msg_" + randomHexID(8)
	}
	return map[string]any{
		"type":   "message",
		"id":     id,
		"role":   "assistant",
		"status": status,
		"content": []any{
			map[string]any{
				"type": "output_text",
				"text": content,
			},
		},
	}
}

func chatToolCallOutputItem(toolCall responsesChatToolCall, status string) map[string]any {
	if toolCall.ItemID == "" {
		toolCall.ItemID = "fc_" + randomHexID(8)
	}
	if toolCall.CallID == "" {
		toolCall.CallID = "call_" + randomHexID(8)
	}
	if toolCall.Arguments == "" {
		toolCall.Arguments = "{}"
	}
	return map[string]any{
		"type":      "function_call",
		"id":        toolCall.ItemID,
		"call_id":   toolCall.CallID,
		"name":      toolCall.Name,
		"arguments": toolCall.Arguments,
		"status":    status,
	}
}

func chatUsageToOpenAIUsage(usage *apicompat.ChatUsage) OpenAIUsage {
	if usage == nil {
		return OpenAIUsage{}
	}
	out := OpenAIUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
	}
	if usage.PromptTokensDetails != nil {
		out.CacheReadInputTokens = usage.PromptTokensDetails.CachedTokens
	}
	return out
}

func randomHexID(n int) string {
	if n <= 0 {
		n = 12
	}
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func firstNonEmptyGJSON(root gjson.Result, paths ...string) string {
	for _, path := range paths {
		if value := root.Get(path).String(); value != "" {
			return value
		}
	}
	return ""
}

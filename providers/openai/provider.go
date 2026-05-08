// Package openai provides the OpenAI provider for aisdk-go.
//
// Import this package to register the OpenAI provider:
//
//	import _ "github.com/sadhakbj/aisdk-go/providers/openai"
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	aisdk "github.com/sadhakbj/aisdk-go"
)

func init() {
	aisdk.RegisterProvider("openai", func(cfg aisdk.ProviderConfig) (aisdk.Provider, error) {
		c, ok := cfg.(*Config)
		if !ok {
			return nil, fmt.Errorf("openai: expected *openai.Config, got %T", cfg)
		}
		return NewProvider(c), nil
	})
}

// Provider implements aisdk.Provider for OpenAI.
type Provider struct {
	config *Config
	client *http.Client
}

// NewProvider creates a new OpenAI provider.
func NewProvider(config *Config) *Provider {
	return &Provider{
		config: config,
		client: &http.Client{},
	}
}

func (p *Provider) ID() string { return "openai" }

// DefaultModel returns the default OpenAI model (default: gpt-5.4).
func (p *Provider) DefaultModel() string { return p.config.defaultModel() }

// SmartModel returns the most capable OpenAI model (gpt-5.4-pro).
func (p *Provider) SmartModel() string { return "gpt-5.4-pro" }

// FastModel returns the cheapest/fastest OpenAI model (gpt-5.4-nano).
func (p *Provider) FastModel() string { return "gpt-5.4-nano" }

func (p *Provider) TextModel(model string) aisdk.TextModel {
	return &textModel{provider: p, model: model}
}

// --- TextModel implementation ---

type textModel struct {
	provider *Provider
	model    string
}

func (m *textModel) Generate(ctx context.Context, req *aisdk.TextRequest) (*aisdk.TextResult, error) {
	// WebSearch requires the Responses API — a different endpoint and format.
	if hasWebSearch(req.BuiltinTools) {
		return m.generateWithResponses(ctx, req)
	}

	body := m.buildRequestBody(req, false)

	respBody, err := m.doRequest(ctx, m.provider.config.baseURL()+"/chat/completions", body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var apiResp chatCompletionResponse
	if err := json.NewDecoder(respBody).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("openai: failed to decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return &aisdk.TextResult{
			FinishReason: aisdk.FinishUnknown,
			Usage:        convertUsage(apiResp.Usage),
		}, nil
	}

	choice := apiResp.Choices[0]
	result := &aisdk.TextResult{
		Content:      choice.Message.Content,
		FinishReason: mapFinishReason(choice.FinishReason),
		Usage:        convertUsage(apiResp.Usage),
	}

	for _, tc := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, aisdk.ToolCallData{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments),
		})
	}

	return result, nil
}

func (m *textModel) Stream(ctx context.Context, req *aisdk.TextRequest) (*aisdk.TextStreamResult, error) {
	// WebSearch requires the Responses API — a different endpoint and format.
	if hasWebSearch(req.BuiltinTools) {
		return m.streamWithResponses(ctx, req)
	}

	body := m.buildRequestBody(req, true)

	respBody, err := m.doRequest(ctx, m.provider.config.baseURL()+"/chat/completions", body)
	if err != nil {
		return nil, err
	}

	ch := make(chan aisdk.StreamEvent)

	go func() {
		defer close(ch)
		defer respBody.Close()
		m.parseSSEStream(respBody, ch)
	}()

	return &aisdk.TextStreamResult{Events: ch}, nil
}

// --- Chat Completions (standard, no web search) ---

func (m *textModel) buildRequestBody(req *aisdk.TextRequest, stream bool) map[string]any {
	body := map[string]any{
		"model": m.model,
	}

	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]any{"include_usage": true}
	}

	// Build messages
	var messages []map[string]any

	if req.System != "" {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": req.System,
		})
	}

	for _, msg := range req.Messages {
		m := map[string]any{
			"role": string(msg.Role),
		}
		if msg.Content != "" {
			m["content"] = msg.Content
		}
		if msg.ToolCallID != "" {
			m["tool_call_id"] = msg.ToolCallID
		}
		if len(msg.ToolCalls) > 0 {
			var toolCalls []map[string]any
			for _, tc := range msg.ToolCalls {
				toolCalls = append(toolCalls, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": string(tc.Arguments),
					},
				})
			}
			m["tool_calls"] = toolCalls
		}
		messages = append(messages, m)
	}

	body["messages"] = messages

	// Tools (client-side functions only)
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
		body["tools"] = tools
	}

	// Optional parameters
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		body["stop"] = req.StopSequences
	}

	// Response format (structured output)
	if req.ResponseFormat != nil {
		switch req.ResponseFormat.Type {
		case "json_object":
			body["response_format"] = map[string]any{"type": "json_object"}
		case "json_schema":
			body["response_format"] = map[string]any{
				"type": "json_schema",
				"json_schema": map[string]any{
					"name":   "response",
					"strict": true,
					"schema": req.ResponseFormat.JSONSchema,
				},
			}
		}
	}

	return body
}

// --- Responses API (used for WebSearch) ---

// generateWithResponses calls POST /v1/responses with the built-in web_search tool.
// The Responses API works with the configured model and lets
// the model decide when to search, unlike Chat Completions + web_search_options.
func (m *textModel) generateWithResponses(ctx context.Context, req *aisdk.TextRequest) (*aisdk.TextResult, error) {
	body := m.buildResponsesBody(req)

	respBody, err := m.doRequest(ctx, m.provider.config.baseURL()+"/responses", body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var apiResp responsesAPIResponse
	if err := json.NewDecoder(respBody).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("openai: failed to decode responses API response: %w", err)
	}

	return parseResponsesResult(&apiResp), nil
}

// streamWithResponses calls POST /v1/responses with stream:true.
func (m *textModel) streamWithResponses(ctx context.Context, req *aisdk.TextRequest) (*aisdk.TextStreamResult, error) {
	body := m.buildResponsesBody(req)
	body["stream"] = true

	respBody, err := m.doRequest(ctx, m.provider.config.baseURL()+"/responses", body)
	if err != nil {
		return nil, err
	}

	ch := make(chan aisdk.StreamEvent)

	go func() {
		defer close(ch)
		defer respBody.Close()
		parseResponsesSSEStream(respBody, ch)
	}()

	return &aisdk.TextStreamResult{Events: ch}, nil
}

func (m *textModel) buildResponsesBody(req *aisdk.TextRequest) map[string]any {
	body := map[string]any{
		"model": m.model,
	}

	// System prompt is "instructions" in the Responses API
	if req.System != "" {
		body["instructions"] = req.System
	}

	// Build input (messages)
	var input []map[string]any
	for _, msg := range req.Messages {
		switch msg.Role {
		case aisdk.RoleUser:
			input = append(input, map[string]any{
				"role":    "user",
				"content": msg.Content,
			})
		case aisdk.RoleAssistant:
			input = append(input, map[string]any{
				"role":    "assistant",
				"content": msg.Content,
			})
		}
	}
	body["input"] = input

	// Built-in tools
	var tools []map[string]any
	for _, bt := range req.BuiltinTools {
		switch v := bt.(type) {
		case *aisdk.WebSearch:
			tool := map[string]any{"type": "web_search"}
			if len(v.AllowedDomains) > 0 {
				tool["filters"] = map[string]any{"allowed_domains": v.AllowedDomains}
			}
			if v.UserLocation != nil {
				loc := map[string]any{"type": "approximate"}
				if v.UserLocation.Country != "" {
					loc["country"] = v.UserLocation.Country
				}
				if v.UserLocation.City != "" {
					loc["city"] = v.UserLocation.City
				}
				if v.UserLocation.Region != "" {
					loc["region"] = v.UserLocation.Region
				}
				if v.UserLocation.Timezone != "" {
					loc["timezone"] = v.UserLocation.Timezone
				}
				tool["user_location"] = loc
			}
			tools = append(tools, tool)
		}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_output_tokens"] = *req.MaxTokens
	}

	return body
}

// parseResponsesResult extracts text and usage from a Responses API response.
// It skips web_search_call output items and reads only message output items.
func parseResponsesResult(resp *responsesAPIResponse) *aisdk.TextResult {
	result := &aisdk.TextResult{
		FinishReason: aisdk.FinishStop,
		Usage: aisdk.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}

	for _, item := range resp.Output {
		if item.Type == "message" {
			for _, part := range item.Content {
				if part.Type == "output_text" {
					result.Content += part.Text
				}
			}
		}
	}

	return result
}

// parseResponsesSSEStream parses the Responses API SSE stream.
func parseResponsesSSEStream(body io.Reader, ch chan<- aisdk.StreamEvent) {
	scanner := bufio.NewScanner(body)

	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		var event responsesSSEEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				ch <- &aisdk.TextDelta{Text: event.Delta}
			}
		case "response.completed":
			if event.Response != nil {
				usage := aisdk.Usage{
					PromptTokens:     event.Response.Usage.InputTokens,
					CompletionTokens: event.Response.Usage.OutputTokens,
					TotalTokens:      event.Response.Usage.TotalTokens,
				}
				ch <- &aisdk.StreamEnd{FinishReason: aisdk.FinishStop, Usage: usage}
			}
		case "response.failed", "error":
			ch <- &aisdk.ErrorEvent{
				Err:         fmt.Errorf("openai responses stream error: %s", event.Type),
				Recoverable: false,
			}
			return
		}
	}
}

// --- Shared HTTP helper ---

func (m *textModel) doRequest(ctx context.Context, url string, body map[string]any) (io.ReadCloser, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("openai: failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.provider.config.APIKey)
	if m.provider.config.Organization != "" {
		httpReq.Header.Set("OpenAI-Organization", m.provider.config.Organization)
	}

	resp, err := m.provider.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)

		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			return nil, aisdk.NewRateLimitedError("openai", 0, fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes)))
		case http.StatusServiceUnavailable, http.StatusBadGateway:
			return nil, aisdk.NewProviderOverloadedError("openai", fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes)))
		default:
			return nil, aisdk.NewProviderError("openai", resp.StatusCode, string(bodyBytes), nil)
		}
	}

	return resp.Body, nil
}

// --- SSE stream parsing (Chat Completions) ---

func (m *textModel) parseSSEStream(body io.Reader, ch chan<- aisdk.StreamEvent) {
	scanner := bufio.NewScanner(body)

	// Track tool calls being built up incrementally
	toolCalls := map[int]*toolCallAccumulator{}

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}

		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			ch <- &aisdk.ErrorEvent{Err: fmt.Errorf("openai: failed to parse SSE chunk: %w", err), Recoverable: true}
			continue
		}

		if len(chunk.Choices) == 0 {
			// Usage-only chunk (sent at the end with stream_options)
			if chunk.Usage != nil {
				ch <- &aisdk.StreamEnd{
					FinishReason: aisdk.FinishStop,
					Usage:        convertUsagePtr(chunk.Usage),
				}
			}
			continue
		}

		choice := chunk.Choices[0]
		delta := choice.Delta

		// Text content
		if delta.Content != "" {
			ch <- &aisdk.TextDelta{Text: delta.Content}
		}

		// Tool calls (streamed incrementally)
		for _, tc := range delta.ToolCalls {
			acc, exists := toolCalls[tc.Index]
			if !exists {
				acc = &toolCallAccumulator{ID: tc.ID, Name: tc.Function.Name}
				toolCalls[tc.Index] = acc
			}
			acc.Arguments += tc.Function.Arguments
		}

		// Finish reason
		if choice.FinishReason != "" {
			reason := mapFinishReason(choice.FinishReason)

			// Emit accumulated tool calls
			if reason == aisdk.FinishToolCalls {
				for _, acc := range toolCalls {
					ch <- &aisdk.ToolCallEvent{
						ID:   acc.ID,
						Name: acc.Name,
						Args: json.RawMessage(acc.Arguments),
					}
				}
			}

			// StreamEnd with usage if available
			var usage aisdk.Usage
			if chunk.Usage != nil {
				usage = convertUsagePtr(chunk.Usage)
			}
			ch <- &aisdk.StreamEnd{FinishReason: reason, Usage: usage}
		}
	}
}

// --- API types: Chat Completions ---

type chatCompletionResponse struct {
	ID      string   `json:"id"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatCompletionChunk struct {
	ID      string        `json:"id"`
	Choices []chunkChoice `json:"choices"`
	Usage   *usage        `json:"usage,omitempty"`
}

type chunkChoice struct {
	Delta        chunkDelta `json:"delta"`
	FinishReason string     `json:"finish_reason"`
}

type chunkDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []chunkToolCall `json:"tool_calls,omitempty"`
}

type chunkToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function functionCall `json:"function"`
}

type toolCallAccumulator struct {
	ID        string
	Name      string
	Arguments string
}

// --- API types: Responses API ---

type responsesAPIResponse struct {
	ID     string               `json:"id"`
	Output []responsesOutputItem `json:"output"`
	Usage  responsesUsage       `json:"usage"`
}

type responsesOutputItem struct {
	Type    string                `json:"type"` // "message" | "web_search_call"
	Content []responsesContentPart `json:"content,omitempty"`
}

type responsesContentPart struct {
	Type string `json:"type"` // "output_text"
	Text string `json:"text,omitempty"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type responsesSSEEvent struct {
	Type     string                `json:"type"`
	Delta    string                `json:"delta,omitempty"`
	Response *responsesAPIResponse `json:"response,omitempty"`
}

// --- Helpers ---

func convertUsage(u usage) aisdk.Usage {
	return aisdk.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
}

func convertUsagePtr(u *usage) aisdk.Usage {
	if u == nil {
		return aisdk.Usage{}
	}
	return convertUsage(*u)
}

func mapFinishReason(reason string) aisdk.FinishReason {
	switch reason {
	case "stop":
		return aisdk.FinishStop
	case "tool_calls":
		return aisdk.FinishToolCalls
	case "length":
		return aisdk.FinishLength
	case "content_filter":
		return aisdk.FinishContentFilter
	default:
		return aisdk.FinishUnknown
	}
}

// hasWebSearch reports whether any builtin tool is a WebSearch.
func hasWebSearch(tools []aisdk.BuiltinTool) bool {
	for _, bt := range tools {
		if _, ok := bt.(*aisdk.WebSearch); ok {
			return true
		}
	}
	return false
}

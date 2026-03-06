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

// DefaultModel returns the default OpenAI model.
// Like Laravel's defaultTextModel() → 'gpt-4o'.
func (p *Provider) DefaultModel() string { return p.config.defaultModel() }

// SmartModel returns the most capable OpenAI model.
// Like Laravel's smartestTextModel() → 'gpt-4o'.
func (p *Provider) SmartModel() string { return "gpt-4o" }

// FastModel returns the cheapest/fastest OpenAI model.
// Like Laravel's cheapestTextModel() → 'gpt-4o-mini'.
func (p *Provider) FastModel() string { return "gpt-4o-mini" }

func (p *Provider) TextModel(model string) aisdk.TextModel {
	return &textModel{provider: p, model: model}
}

// --- TextModel implementation ---

type textModel struct {
	provider *Provider
	model    string
}

func (m *textModel) Generate(ctx context.Context, req *aisdk.TextRequest) (*aisdk.TextResult, error) {
	body := m.buildRequestBody(req, false)

	respBody, err := m.doRequest(ctx, body)
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
	body := m.buildRequestBody(req, true)

	respBody, err := m.doRequest(ctx, body)
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

// --- Request building ---

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

	// Tools
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

func (m *textModel) doRequest(ctx context.Context, body map[string]any) (io.ReadCloser, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: failed to marshal request: %w", err)
	}

	url := m.provider.config.baseURL() + "/chat/completions"
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

// --- SSE stream parsing ---

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

// --- API types ---

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
	Role      string           `json:"role,omitempty"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []chunkToolCall  `json:"tool_calls,omitempty"`
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

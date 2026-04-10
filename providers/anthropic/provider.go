// Package anthropic provides the Anthropic provider for aisdk-go.
//
// Import this package to register the Anthropic provider:
//
//	import _ "github.com/sadhakbj/aisdk-go/providers/anthropic"
package anthropic

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
	aisdk.RegisterProvider("anthropic", func(cfg aisdk.ProviderConfig) (aisdk.Provider, error) {
		c, ok := cfg.(*Config)
		if !ok {
			return nil, fmt.Errorf("anthropic: expected *anthropic.Config, got %T", cfg)
		}
		return NewProvider(c), nil
	})
}

// Provider implements aisdk.Provider for Anthropic.
type Provider struct {
	config *Config
	client *http.Client
}

// NewProvider creates a new Anthropic provider.
func NewProvider(config *Config) *Provider {
	return &Provider{
		config: config,
		client: &http.Client{},
	}
}

func (p *Provider) ID() string { return "anthropic" }

// DefaultModel returns the default Anthropic model.
// Like Laravel's defaultTextModel() → 'claude-sonnet-4-20250514'.
func (p *Provider) DefaultModel() string { return p.config.defaultModel() }

// SmartModel returns the most capable Anthropic model.
// Like Laravel's smartestTextModel() → 'claude-sonnet-4-20250514'.
func (p *Provider) SmartModel() string { return "claude-sonnet-4-20250514" }

// FastModel returns the cheapest/fastest Anthropic model.
// Like Laravel's cheapestTextModel() → 'claude-haiku-3-5-20241022'.
func (p *Provider) FastModel() string { return "claude-haiku-3-5-20241022" }

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

	var apiResp messagesResponse
	if err := json.NewDecoder(respBody).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("anthropic: failed to decode response: %w", err)
	}

	result := &aisdk.TextResult{
		FinishReason: mapStopReason(apiResp.StopReason),
		Usage:        convertUsage(apiResp.Usage),
	}

	for _, block := range apiResp.Content {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			result.ToolCalls = append(result.ToolCalls, aisdk.ToolCallData{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: argsJSON,
			})
		}
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
	}

	// System prompt
	if req.System != "" {
		body["system"] = req.System
	}

	// Max tokens (required by Anthropic)
	maxTokens := 4096
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}
	body["max_tokens"] = maxTokens

	// Build messages
	var messages []map[string]any
	for _, msg := range req.Messages {
		switch msg.Role {
		case aisdk.RoleUser:
			messages = append(messages, map[string]any{
				"role":    "user",
				"content": msg.Content,
			})
		case aisdk.RoleAssistant:
			content := m.buildAssistantContent(msg)
			messages = append(messages, map[string]any{
				"role":    "assistant",
				"content": content,
			})
		case aisdk.RoleTool:
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []map[string]any{
					{
						"type":        "tool_result",
						"tool_use_id": msg.ToolCallID,
						"content":     msg.Content,
					},
				},
			})
		case aisdk.RoleSystem:
			// Anthropic uses top-level system, skip system messages in the array
			continue
		}
	}
	body["messages"] = messages

	// Tools
	var tools []map[string]any
	for _, t := range req.Tools {
		tools = append(tools, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.Parameters,
		})
	}
	for _, bt := range req.BuiltinTools {
		switch v := bt.(type) {
		case *aisdk.WebSearch:
			tool := map[string]any{
				"type": "web_search_20250305",
				"name": "web_search",
			}
			if v.MaxResults > 0 {
				tool["max_uses"] = v.MaxResults
			}
			if len(v.AllowedDomains) > 0 {
				tool["allowed_domains"] = v.AllowedDomains
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
				tool["user_location"] = loc
			}
			tools = append(tools, tool)
		}
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	// Optional parameters
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		body["stop_sequences"] = req.StopSequences
	}

	return body
}

func (m *textModel) buildAssistantContent(msg aisdk.Message) any {
	if len(msg.ToolCalls) == 0 {
		return msg.Content
	}

	// Anthropic represents tool calls as content blocks
	var blocks []map[string]any
	if msg.Content != "" {
		blocks = append(blocks, map[string]any{
			"type": "text",
			"text": msg.Content,
		})
	}
	for _, tc := range msg.ToolCalls {
		var input any
		_ = json.Unmarshal(tc.Arguments, &input)
		blocks = append(blocks, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Name,
			"input": input,
		})
	}
	return blocks
}

func (m *textModel) doRequest(ctx context.Context, body map[string]any) (io.ReadCloser, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: failed to marshal request: %w", err)
	}

	url := m.provider.config.baseURL() + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("anthropic: failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", m.provider.config.APIKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := m.provider.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)

		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			return nil, aisdk.NewRateLimitedError("anthropic", 0, fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes)))
		case http.StatusServiceUnavailable, http.StatusBadGateway:
			return nil, aisdk.NewProviderOverloadedError("anthropic", fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes)))
		case 529: // Anthropic overloaded
			return nil, aisdk.NewProviderOverloadedError("anthropic", fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes)))
		default:
			return nil, aisdk.NewProviderError("anthropic", resp.StatusCode, string(bodyBytes), nil)
		}
	}

	return resp.Body, nil
}

// --- SSE stream parsing ---

func (m *textModel) parseSSEStream(body io.Reader, ch chan<- aisdk.StreamEvent) {
	scanner := bufio.NewScanner(body)

	// Increase buffer size for large responses
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	// Track tool use blocks being built
	var currentToolID string
	var currentToolName string
	var currentToolArgs string
	var totalUsage aisdk.Usage

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var event sseEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_start":
			if event.ContentBlock != nil {
				switch event.ContentBlock.Type {
				case "tool_use":
					currentToolID = event.ContentBlock.ID
					currentToolName = event.ContentBlock.Name
					currentToolArgs = ""
				case "thinking":
					// Reasoning block started
				}
			}

		case "content_block_delta":
			if event.Delta != nil {
				switch event.Delta.Type {
				case "text_delta":
					ch <- &aisdk.TextDelta{Text: event.Delta.Text}
				case "thinking_delta":
					ch <- &aisdk.ReasoningDelta{Text: event.Delta.Thinking}
				case "input_json_delta":
					currentToolArgs += event.Delta.PartialJSON
				}
			}

		case "content_block_stop":
			// If we were building a tool call, emit it
			if currentToolID != "" {
				ch <- &aisdk.ToolCallEvent{
					ID:   currentToolID,
					Name: currentToolName,
					Args: json.RawMessage(currentToolArgs),
				}
				currentToolID = ""
				currentToolName = ""
				currentToolArgs = ""
			}

		case "message_delta":
			if event.Delta != nil {
				reason := mapStopReason(event.Delta.StopReason)
				if event.Usage != nil {
					totalUsage = aisdk.Usage{
						PromptTokens:     event.Usage.InputTokens,
						CompletionTokens: event.Usage.OutputTokens,
						TotalTokens:      event.Usage.InputTokens + event.Usage.OutputTokens,
					}
				}
				ch <- &aisdk.StreamEnd{FinishReason: reason, Usage: totalUsage}
			}

		case "message_start":
			if event.Message != nil && event.Message.Usage != nil {
				totalUsage = aisdk.Usage{
					PromptTokens: event.Message.Usage.InputTokens,
					TotalTokens:  event.Message.Usage.InputTokens,
				}
			}

		case "error":
			var errMsg string
			if event.Error != nil {
				errMsg = event.Error.Message
			}
			ch <- &aisdk.ErrorEvent{
				Err:         fmt.Errorf("anthropic stream error: %s", errMsg),
				Recoverable: false,
			}
			return
		}
	}
}

// --- API types ---

type messagesResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      apiUsage       `json:"usage"`
}

type contentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

type apiUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type sseEvent struct {
	Type         string        `json:"type"`
	Delta        *sseDelta     `json:"delta,omitempty"`
	ContentBlock *contentBlock `json:"content_block,omitempty"`
	Message      *sseMessage   `json:"message,omitempty"`
	Usage        *apiUsage     `json:"usage,omitempty"`
	Error        *sseError     `json:"error,omitempty"`
	Index        int           `json:"index,omitempty"`
}

type sseDelta struct {
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type sseMessage struct {
	ID    string   `json:"id"`
	Usage *apiUsage `json:"usage,omitempty"`
}

type sseError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// --- Helpers ---

func convertUsage(u apiUsage) aisdk.Usage {
	return aisdk.Usage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.InputTokens + u.OutputTokens,
	}
}

func mapStopReason(reason string) aisdk.FinishReason {
	switch reason {
	case "end_turn", "stop":
		return aisdk.FinishStop
	case "tool_use":
		return aisdk.FinishToolCalls
	case "max_tokens":
		return aisdk.FinishLength
	default:
		if reason == "" {
			return aisdk.FinishUnknown
		}
		return aisdk.FinishUnknown
	}
}

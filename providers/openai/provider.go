// Package openai provides the OpenAI provider for aisdk-go.
//
// All text generation routes through the modern Responses API
// (POST /v1/responses). The legacy Chat Completions endpoint is no longer
// used because the gpt-5.4 family and newer reasoning-tier models are only
// served by /v1/responses.
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

// Generate performs a synchronous /v1/responses request.
func (m *textModel) Generate(ctx context.Context, req *aisdk.TextRequest) (*aisdk.TextResult, error) {
	body := m.buildBody(req)

	respBody, err := m.doRequest(ctx, m.provider.config.baseURL()+"/responses", body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var apiResp responsesAPIResponse
	if err := json.NewDecoder(respBody).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("openai: failed to decode response: %w", err)
	}

	if apiResp.Status == "failed" && apiResp.Error != nil {
		return nil, fmt.Errorf("openai: response failed: %s", apiResp.Error.Message)
	}

	return parseResponsesResult(&apiResp), nil
}

// Stream performs a streaming /v1/responses request.
func (m *textModel) Stream(ctx context.Context, req *aisdk.TextRequest) (*aisdk.TextStreamResult, error) {
	body := m.buildBody(req)
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

// --- Request body construction ---

// buildBody assembles the JSON payload for POST /v1/responses.
func (m *textModel) buildBody(req *aisdk.TextRequest) map[string]any {
	body := map[string]any{
		"model": m.model,
		"input": m.mapMessages(req),
	}

	if req.System != "" {
		body["instructions"] = req.System
	}

	tools := m.mapTools(req)
	if len(tools) > 0 {
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}

	if req.Temperature != nil && modelSupportsTemperature(m.model) {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_output_tokens"] = *req.MaxTokens
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}

	if req.ResponseFormat != nil {
		body["text"] = m.mapResponseFormat(req.ResponseFormat)
	}

	return body
}

// mapMessages converts our Message slice into the Responses API input array.
//
// Layout (matches OpenAI's /v1/responses input contract):
//
//	user      → {role:"user",      content:[{type:"input_text", text:...}]}
//	assistant → {role:"assistant", content:[{type:"output_text", text:...}]}
//	          + zero or more {type:"function_call", id, call_id, name, arguments}
//	tool      → {type:"function_call_output", call_id, output:"<string>"}
//
// (System prompt is sent separately via the top-level "instructions" field.)
func (m *textModel) mapMessages(req *aisdk.TextRequest) []map[string]any {
	var input []map[string]any

	for _, msg := range req.Messages {
		switch msg.Role {
		case aisdk.RoleSystem:
			// Responses API has a top-level "instructions" field; if a system
			// message slipped into the slice, fold it in as a system input.
			input = append(input, map[string]any{
				"role":    "system",
				"content": msg.Content,
			})

		case aisdk.RoleUser:
			input = append(input, map[string]any{
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": msg.Content},
				},
			})

		case aisdk.RoleAssistant:
			if msg.Content != "" {
				input = append(input, map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": msg.Content},
					},
				})
			}
			for _, tc := range msg.ToolCalls {
				item := map[string]any{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Name,
					"arguments": string(tc.Arguments),
				}
				// Only include the provider-side id when we have it; OpenAI
				// validates the format ("fc_..."), so sending our call_id here
				// would be rejected.
				if tc.ProviderID != "" {
					item["id"] = tc.ProviderID
				}
				input = append(input, item)
			}

		case aisdk.RoleTool:
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": msg.ToolCallID,
				"output":  msg.Content,
			})
		}
	}

	return input
}

// mapTools assembles the tools array, combining client-side function tools
// with provider-native tools (e.g. WebSearch).
func (m *textModel) mapTools(req *aisdk.TextRequest) []map[string]any {
	var tools []map[string]any

	for _, t := range req.Tools {
		tools = append(tools, map[string]any{
			"type":        "function",
			"name":        t.Name,
			"description": t.Description,
			"strict":      true,
			"parameters":  t.Parameters,
		})
	}

	for _, bt := range req.BuiltinTools {
		switch v := bt.(type) {
		case *aisdk.WebSearch:
			tools = append(tools, mapWebSearch(v))
		}
	}

	return tools
}

// mapWebSearch translates aisdk.WebSearch into the Responses API web_search_preview tool.
func mapWebSearch(w *aisdk.WebSearch) map[string]any {
	tool := map[string]any{"type": "web_search_preview"}
	if len(w.AllowedDomains) > 0 {
		tool["filters"] = map[string]any{"allowed_domains": w.AllowedDomains}
	}
	if w.UserLocation != nil {
		loc := map[string]any{"type": "approximate"}
		if w.UserLocation.Country != "" {
			loc["country"] = w.UserLocation.Country
		}
		if w.UserLocation.City != "" {
			loc["city"] = w.UserLocation.City
		}
		if w.UserLocation.Region != "" {
			loc["region"] = w.UserLocation.Region
		}
		if w.UserLocation.Timezone != "" {
			loc["timezone"] = w.UserLocation.Timezone
		}
		tool["user_location"] = loc
	}
	return tool
}

// mapResponseFormat translates our ResponseFormat into the Responses API "text" object.
func (m *textModel) mapResponseFormat(rf *aisdk.ResponseFormat) map[string]any {
	switch rf.Type {
	case "json_object":
		return map[string]any{"format": map[string]any{"type": "json_object"}}
	case "json_schema":
		return map[string]any{
			"format": map[string]any{
				"type":   "json_schema",
				"name":   "response",
				"schema": rf.JSONSchema,
				"strict": true,
			},
		}
	}
	return nil
}

// --- Response parsing (non-streaming) ---

// parseResponsesResult flattens the Responses API output array into a TextResult.
// Output items can be:
//   - {type:"message", content:[{type:"output_text", text:...}]}
//   - {type:"function_call", id, call_id, name, arguments}
//   - {type:"reasoning", id, summary} — informational, ignored
//   - {type:"web_search_call", ...} — informational, ignored
func parseResponsesResult(resp *responsesAPIResponse) *aisdk.TextResult {
	result := &aisdk.TextResult{
		Usage: aisdk.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}

	var lastItemType string
	for _, item := range resp.Output {
		lastItemType = item.Type
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					result.Content += part.Text
				}
			}
		case "function_call":
			result.ToolCalls = append(result.ToolCalls, aisdk.ToolCallData{
				ID:         firstNonEmpty(item.CallID, item.ID),
				ProviderID: item.ID,
				Name:       item.Name,
				Arguments:  json.RawMessage(item.Arguments),
			})
		}
	}

	result.FinishReason = mapResponsesFinishReason(resp.Status, lastItemType, len(result.ToolCalls) > 0)
	return result
}

func mapResponsesFinishReason(status, lastItemType string, hasToolCalls bool) aisdk.FinishReason {
	switch status {
	case "incomplete":
		return aisdk.FinishLength
	case "failed":
		return aisdk.FinishError
	case "completed":
		if hasToolCalls || lastItemType == "function_call" {
			return aisdk.FinishToolCalls
		}
		return aisdk.FinishStop
	}
	if hasToolCalls {
		return aisdk.FinishToolCalls
	}
	return aisdk.FinishUnknown
}

// --- Streaming (SSE) ---

// parseResponsesSSEStream consumes the Responses API event stream and emits
// aisdk.StreamEvents on ch. Reference event types (subset that we care about):
//
//	response.created                       → StreamStart context (no event emitted; agent wraps it)
//	response.output_text.delta             → TextDelta
//	response.output_text.done              → (informational)
//	response.output_item.added             → start tracking a function_call item
//	response.function_call_arguments.delta → accumulate arguments per item
//	response.function_call_arguments.done  → emit ToolCallEvent
//	response.completed                     → StreamEnd (with usage)
//	response.failed | error                → ErrorEvent
func parseResponsesSSEStream(body io.Reader, ch chan<- aisdk.StreamEvent) {
	scanner := bufio.NewScanner(body)

	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	// Pending function_call items keyed by their item id, accumulating arguments.
	type pendingCall struct {
		providerID string // OpenAI's "fc_..." item id; needs to round-trip back
		callID     string // OpenAI's "call_..."; matches function_call_output
		name       string
		arguments  strings.Builder
	}
	pending := map[string]*pendingCall{}

	hasToolCalls := false

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
			continue // skip malformed event lines silently
		}

		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				ch <- &aisdk.TextDelta{Text: event.Delta}
			}

		case "response.output_item.added":
			if event.Item != nil && event.Item.Type == "function_call" {
				pending[event.Item.ID] = &pendingCall{
					providerID: event.Item.ID,
					callID:     firstNonEmpty(event.Item.CallID, event.Item.ID),
					name:       event.Item.Name,
				}
			}

		case "response.function_call_arguments.delta":
			if call, ok := pending[event.ItemID]; ok {
				call.arguments.WriteString(event.Delta)
			}

		case "response.function_call_arguments.done":
			call, ok := pending[event.ItemID]
			if !ok {
				continue
			}
			args := call.arguments.String()
			if event.Arguments != "" {
				args = event.Arguments
			}
			ch <- &aisdk.ToolCallEvent{
				ID:         call.callID,
				ProviderID: call.providerID,
				Name:       call.name,
				Args:       json.RawMessage(args),
			}
			hasToolCalls = true
			delete(pending, event.ItemID)

		case "response.completed":
			usage := aisdk.Usage{}
			finish := aisdk.FinishStop
			if event.Response != nil {
				usage = aisdk.Usage{
					PromptTokens:     event.Response.Usage.InputTokens,
					CompletionTokens: event.Response.Usage.OutputTokens,
					TotalTokens:      event.Response.Usage.TotalTokens,
				}
			}
			if hasToolCalls {
				finish = aisdk.FinishToolCalls
			}
			ch <- &aisdk.StreamEnd{FinishReason: finish, Usage: usage}

		case "response.failed", "error":
			msg := "openai responses stream error"
			if event.Error != nil && event.Error.Message != "" {
				msg = event.Error.Message
			}
			ch <- &aisdk.ErrorEvent{
				Err:         fmt.Errorf("openai: %s", msg),
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

// --- API types: Responses API ---

type responsesAPIResponse struct {
	ID     string                `json:"id"`
	Model  string                `json:"model"`
	Status string                `json:"status"`
	Output []responsesOutputItem `json:"output"`
	Usage  responsesUsage        `json:"usage"`
	Error  *responsesError       `json:"error,omitempty"`
}

type responsesOutputItem struct {
	Type      string                 `json:"type"`
	ID        string                 `json:"id,omitempty"`
	CallID    string                 `json:"call_id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments string                 `json:"arguments,omitempty"`
	Content   []responsesContentPart `json:"content,omitempty"`
}

type responsesContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type responsesError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type responsesSSEEvent struct {
	Type      string                `json:"type"`
	Delta     string                `json:"delta,omitempty"`
	ItemID    string                `json:"item_id,omitempty"`
	Item      *responsesOutputItem  `json:"item,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
	Response  *responsesAPIResponse `json:"response,omitempty"`
	Error     *responsesError       `json:"error,omitempty"`
}

// --- Helpers ---

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// modelSupportsTemperature reports whether the given model accepts a custom
// temperature value. OpenAI's reasoning-tier models (o1/o3/o4 families) and
// the gpt-5 family only accept the default temperature of 1 and reject any
// other value with "Unsupported parameter: 'temperature' is not supported
// with this model".
func modelSupportsTemperature(model string) bool {
	switch {
	case strings.HasPrefix(model, "o1"),
		strings.HasPrefix(model, "o3"),
		strings.HasPrefix(model, "o4"),
		strings.HasPrefix(model, "gpt-5"):
		return false
	}
	return true
}

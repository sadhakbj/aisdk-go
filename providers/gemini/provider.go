// Package gemini provides the Google Gemini provider for aisdk-go.
//
// Import this package to register the Gemini provider:
//
//	import _ "github.com/sadhakbj/aisdk-go/providers/gemini"
package gemini

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
	aisdk.RegisterProvider("gemini", func(cfg aisdk.ProviderConfig) (aisdk.Provider, error) {
		c, ok := cfg.(*Config)
		if !ok {
			return nil, fmt.Errorf("gemini: expected *gemini.Config, got %T", cfg)
		}
		return NewProvider(c), nil
	})
}

// Provider implements aisdk.Provider for Google Gemini.
type Provider struct {
	config *Config
	client *http.Client
}

// NewProvider creates a new Gemini provider.
func NewProvider(config *Config) *Provider {
	return &Provider{
		config: config,
		client: &http.Client{},
	}
}

func (p *Provider) ID() string           { return "gemini" }
func (p *Provider) DefaultModel() string { return p.config.defaultModel() }
func (p *Provider) SmartModel() string   { return "gemini-2.0-flash" }
func (p *Provider) FastModel() string    { return "gemini-2.0-flash-lite" }

func (p *Provider) TextModel(model string) aisdk.TextModel {
	return &textModel{provider: p, model: model}
}

// --- TextModel implementation ---

type textModel struct {
	provider *Provider
	model    string
}

func (m *textModel) Generate(ctx context.Context, req *aisdk.TextRequest) (*aisdk.TextResult, error) {
	body := m.buildRequestBody(req)

	respBody, err := m.doRequest(ctx, body, false)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var apiResp generateContentResponse
	if err := json.NewDecoder(respBody).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("gemini: failed to decode response: %w", err)
	}

	result := &aisdk.TextResult{
		Usage: convertUsage(apiResp.UsageMetadata),
	}

	if len(apiResp.Candidates) == 0 {
		result.FinishReason = aisdk.FinishUnknown
		return result, nil
	}

	candidate := apiResp.Candidates[0]
	result.FinishReason = mapFinishReason(candidate.FinishReason)

	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			result.Content += part.Text
		}
		if part.FunctionCall != nil {
			argsJSON, _ := json.Marshal(part.FunctionCall.Args)
			// Gemini has no call IDs — use function name as ID so ToolResult
			// messages (which reference ToolCallID) can match up.
			result.ToolCalls = append(result.ToolCalls, aisdk.ToolCallData{
				ID:        part.FunctionCall.Name,
				Name:      part.FunctionCall.Name,
				Arguments: argsJSON,
			})
		}
	}

	if len(result.ToolCalls) > 0 {
		result.FinishReason = aisdk.FinishToolCalls
	}

	return result, nil
}

func (m *textModel) Stream(ctx context.Context, req *aisdk.TextRequest) (*aisdk.TextStreamResult, error) {
	body := m.buildRequestBody(req)

	respBody, err := m.doRequest(ctx, body, true)
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

func (m *textModel) buildRequestBody(req *aisdk.TextRequest) map[string]any {
	body := map[string]any{}

	// Collect system text from both req.System and any RoleSystem messages.
	systemText := req.System
	contents := []map[string]any{}

	for _, msg := range req.Messages {
		if msg.Role == aisdk.RoleSystem {
			if systemText != "" {
				systemText += "\n" + msg.Content
			} else {
				systemText = msg.Content
			}
			continue
		}
		contents = append(contents, m.convertMessage(msg))
	}

	if systemText != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": systemText}},
		}
	}

	body["contents"] = contents

	// Tools
	if len(req.Tools) > 0 {
		var funcDecls []map[string]any
		for _, t := range req.Tools {
			funcDecls = append(funcDecls, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			})
		}
		body["tools"] = []map[string]any{
			{"functionDeclarations": funcDecls},
		}
	}

	// Generation config
	genConfig := map[string]any{}
	if req.Temperature != nil {
		genConfig["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		genConfig["maxOutputTokens"] = *req.MaxTokens
	}
	if req.TopP != nil {
		genConfig["topP"] = *req.TopP
	}
	if len(req.StopSequences) > 0 {
		genConfig["stopSequences"] = req.StopSequences
	}
	if req.ResponseFormat != nil {
		switch req.ResponseFormat.Type {
		case "json_object":
			genConfig["responseMimeType"] = "application/json"
		case "json_schema":
			genConfig["responseMimeType"] = "application/json"
			genConfig["responseSchema"] = req.ResponseFormat.JSONSchema
		}
	}
	if len(genConfig) > 0 {
		body["generationConfig"] = genConfig
	}

	return body
}

func (m *textModel) convertMessage(msg aisdk.Message) map[string]any {
	switch msg.Role {
	case aisdk.RoleUser:
		return map[string]any{
			"role":  "user",
			"parts": []map[string]any{{"text": msg.Content}},
		}

	case aisdk.RoleAssistant:
		if len(msg.ToolCalls) == 0 {
			return map[string]any{
				"role":  "model",
				"parts": []map[string]any{{"text": msg.Content}},
			}
		}
		var parts []map[string]any
		if msg.Content != "" {
			parts = append(parts, map[string]any{"text": msg.Content})
		}
		for _, tc := range msg.ToolCalls {
			var args any
			_ = json.Unmarshal(tc.Arguments, &args)
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{
					"name": tc.Name,
					"args": args,
				},
			})
		}
		return map[string]any{"role": "model", "parts": parts}

	case aisdk.RoleTool:
		// ToolCallID holds the function name (ID = function name, see Generate).
		var result any
		if err := json.Unmarshal([]byte(msg.Content), &result); err != nil {
			result = msg.Content
		}
		return map[string]any{
			"role": "user",
			"parts": []map[string]any{
				{
					"functionResponse": map[string]any{
						"name":     msg.ToolCallID,
						"response": map[string]any{"result": result},
					},
				},
			},
		}

	default:
		return map[string]any{
			"role":  "user",
			"parts": []map[string]any{{"text": msg.Content}},
		}
	}
}

func (m *textModel) doRequest(ctx context.Context, body map[string]any, stream bool) (io.ReadCloser, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("gemini: failed to marshal request: %w", err)
	}

	var endpoint string
	if stream {
		endpoint = fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", m.provider.config.baseURL(), m.model)
	} else {
		endpoint = fmt.Sprintf("%s/models/%s:generateContent", m.provider.config.baseURL(), m.model)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("gemini: failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", m.provider.config.APIKey)

	resp, err := m.provider.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			return nil, aisdk.NewRateLimitedError("gemini", 0, fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes)))
		case http.StatusServiceUnavailable, http.StatusBadGateway:
			return nil, aisdk.NewProviderOverloadedError("gemini", fmt.Errorf("status %d: %s", resp.StatusCode, string(bodyBytes)))
		default:
			return nil, aisdk.NewProviderError("gemini", resp.StatusCode, string(bodyBytes), nil)
		}
	}

	return resp.Body, nil
}

// --- SSE stream parsing ---

func (m *textModel) parseSSEStream(body io.Reader, ch chan<- aisdk.StreamEvent) {
	scanner := bufio.NewScanner(body)

	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var totalUsage aisdk.Usage
	var pendingToolCalls []aisdk.ToolCallData

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var chunk generateContentResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.UsageMetadata.TotalTokenCount > 0 {
			totalUsage = convertUsage(chunk.UsageMetadata)
		}

		if len(chunk.Candidates) == 0 {
			continue
		}

		candidate := chunk.Candidates[0]

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				ch <- &aisdk.TextDelta{Text: part.Text}
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				pendingToolCalls = append(pendingToolCalls, aisdk.ToolCallData{
					ID:        part.FunctionCall.Name,
					Name:      part.FunctionCall.Name,
					Arguments: argsJSON,
				})
			}
		}

		if candidate.FinishReason != "" {
			for _, tc := range pendingToolCalls {
				ch <- &aisdk.ToolCallEvent{
					ID:   tc.ID,
					Name: tc.Name,
					Args: tc.Arguments,
				}
			}
			reason := mapFinishReason(candidate.FinishReason)
			if len(pendingToolCalls) > 0 {
				reason = aisdk.FinishToolCalls
			}
			ch <- &aisdk.StreamEnd{FinishReason: reason, Usage: totalUsage}
			return
		}
	}
}

// --- API types ---

type generateContentResponse struct {
	Candidates    []candidate   `json:"candidates"`
	UsageMetadata usageMetadata `json:"usageMetadata"`
}

type candidate struct {
	Content      candidateContent `json:"content"`
	FinishReason string           `json:"finishReason"`
}

type candidateContent struct {
	Parts []part `json:"parts"`
	Role  string `json:"role"`
}

type part struct {
	Text         string        `json:"text,omitempty"`
	FunctionCall *functionCall `json:"functionCall,omitempty"`
}

type functionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// --- Helpers ---

func convertUsage(u usageMetadata) aisdk.Usage {
	return aisdk.Usage{
		PromptTokens:     u.PromptTokenCount,
		CompletionTokens: u.CandidatesTokenCount,
		TotalTokens:      u.TotalTokenCount,
	}
}

func mapFinishReason(reason string) aisdk.FinishReason {
	switch reason {
	case "STOP":
		return aisdk.FinishStop
	case "MAX_TOKENS":
		return aisdk.FinishLength
	case "SAFETY":
		return aisdk.FinishContentFilter
	default:
		return aisdk.FinishUnknown
	}
}

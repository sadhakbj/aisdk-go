package aisdk

import "context"

// Provider represents an AI provider (OpenAI, Anthropic, etc.).
// Implement this interface to add a new provider to the SDK.
//
// Each provider defines its own model tiers — just like Laravel's
// defaultTextModel(), smartestTextModel(), and cheapestTextModel().
// Users never need to configure model names; the provider knows its own models.
type Provider interface {
	// ID returns a unique identifier for this provider (e.g. "openai", "anthropic").
	ID() string

	// TextModel returns a TextModel for the given model name.
	TextModel(model string) TextModel

	// DefaultModel returns the default model name for this provider.
	// Like Laravel's defaultTextModel().
	DefaultModel() string

	// SmartModel returns the most capable model for this provider.
	// Like Laravel's smartestTextModel().
	SmartModel() string

	// FastModel returns the cheapest/fastest model for this provider.
	// Like Laravel's cheapestTextModel().
	FastModel() string
}

// TextModel is the core generation interface. Each provider implements this
// for its text/chat completion API.
type TextModel interface {
	// Generate performs a synchronous text generation request.
	Generate(ctx context.Context, req *TextRequest) (*TextResult, error)

	// Stream performs a streaming text generation request.
	Stream(ctx context.Context, req *TextRequest) (*TextStreamResult, error)
}

// ProviderConfig is a marker interface for provider-specific configuration.
// Each provider package defines its own Config struct implementing this.
type ProviderConfig interface {
	IsProviderConfig()
}

// TextRequest is the unified request sent to a TextModel.
type TextRequest struct {
	Model        string    `json:"model"`
	System       string    `json:"system,omitempty"`
	Messages     []Message `json:"messages"`
	Tools        []ToolDef `json:"tools,omitempty"`
	Temperature  *float64  `json:"temperature,omitempty"`
	MaxTokens    *int      `json:"max_tokens,omitempty"`
	TopP         *float64  `json:"top_p,omitempty"`
	StopSequences []string `json:"stop,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// ToolDef is the JSON-serializable definition of a tool sent to the provider.
type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // JSON Schema object
}

// ResponseFormat controls structured output mode.
type ResponseFormat struct {
	Type       string `json:"type"` // "json_object" or "json_schema"
	JSONSchema any    `json:"json_schema,omitempty"`
}

// TextResult is the raw result from a TextModel.Generate call.
type TextResult struct {
	Content      string         `json:"content"`
	ToolCalls    []ToolCallData `json:"tool_calls,omitempty"`
	FinishReason FinishReason   `json:"finish_reason"`
	Usage        Usage          `json:"usage"`
}

// TextStreamResult holds the channel and metadata for a streaming response.
type TextStreamResult struct {
	// Events is a channel that receives stream events.
	// The channel is closed when the stream ends.
	Events <-chan StreamEvent
}

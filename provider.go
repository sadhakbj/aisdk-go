package aisdk

import (
	"context"
	"encoding/json"
	"fmt"
)

// Provider represents an AI provider (OpenAI, Anthropic, etc.).
// Implement this interface to add a new provider to the SDK.
//
// Each provider defines its own model tiers (default, smart, fast).
// Callers use aliases such as "default", "smart", and "fast"; the provider maps them to concrete model IDs.
type Provider interface {
	// ID returns a unique identifier for this provider (e.g. "openai", "anthropic").
	ID() string

	// TextModel returns a TextModel for the given model name.
	TextModel(model string) TextModel

	// DefaultModel returns the default model name for this provider.
	DefaultModel() string

	// SmartModel returns the most capable model for this provider.
	SmartModel() string

	// FastModel returns the cheapest/fastest model for this provider.
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
	Model          string       `json:"model"`
	System         string       `json:"system,omitempty"`
	Messages       []Message    `json:"messages"`
	Tools          []ToolDef    `json:"tools,omitzero"`
	BuiltinTools   []BuiltinTool `json:"-"`
	Temperature    *float64     `json:"temperature,omitempty"`
	MaxTokens      *int         `json:"max_tokens,omitempty"`
	TopP           *float64     `json:"top_p,omitempty"`
	StopSequences  []string     `json:"stop,omitzero"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// ToolDef is the JSON-serializable definition of a tool sent to the provider.
type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"` // JSON Schema object
}

// BuiltinTool marks a tool that is executed natively by the AI provider's
// servers, not by client code. Unlike Tool, there is no Execute method —
// the provider translates it to its own API format.
type BuiltinTool interface {
	builtinToolName() string
}

// WebSearch enables the AI provider's built-in web search capability.
// No third-party API key required — the provider handles searching server-side.
//
// Usage:
//
//	agent := aisdk.NewBaseAgent(aisdk.AgentConfig{
//	    BuiltinTools: []aisdk.BuiltinTool{
//	        &aisdk.WebSearch{
//	            AllowedDomains: []string{"pubmed.ncbi.nlm.nih.gov", "who.int"},
//	            UserLocation:   &aisdk.WebSearchLocation{Country: "US", City: "New York"},
//	        },
//	    },
//	})
type WebSearch struct {
	// MaxResults limits how many searches the provider may make (Anthropic: max_uses).
	MaxResults int
	// AllowedDomains restricts results to specific domains.
	// Anthropic: top-level allowed_domains. OpenAI: filters.allowed_domains.
	AllowedDomains []string
	// UserLocation refines results based on the user's approximate location.
	// Supported by both Anthropic and OpenAI.
	UserLocation *WebSearchLocation
}

// WebSearchLocation provides geographic context to refine web search results.
type WebSearchLocation struct {
	// Country is the two-letter ISO country code (e.g. "US", "GB").
	Country string
	// City is the user's city.
	City string
	// Region is the user's region or state.
	Region string
	// Timezone is the IANA timezone string (e.g. "America/New_York"). OpenAI only.
	Timezone string
}

func (w *WebSearch) builtinToolName() string { return "web_search" }

// WebSearch implements Tool so it can be added to AgentConfig.Tools alongside
// regular client-side tools. The agent detects it as a BuiltinTool and sends
// it to the provider as a native capability rather than a function definition.

func (w *WebSearch) Name() string        { return "web_search" }
func (w *WebSearch) Description() string { return "Search the web for current information." }
func (w *WebSearch) Parameters() any     { return nil }
func (w *WebSearch) Execute(_ context.Context, _ json.RawMessage) (any, error) {
	return nil, fmt.Errorf("web_search is executed server-side by the AI provider")
}

// ResponseFormat controls structured output mode.
type ResponseFormat struct {
	Type       string `json:"type"` // "json_object" or "json_schema"
	JSONSchema any    `json:"json_schema,omitzero"`
}

// TextResult is the raw result from a TextModel.Generate call.
type TextResult struct {
	Content      string         `json:"content"`
	ToolCalls    []ToolCallData `json:"tool_calls,omitzero"`
	FinishReason FinishReason   `json:"finish_reason"`
	Usage        Usage          `json:"usage,omitzero"`
}

// TextStreamResult holds the channel and metadata for a streaming response.
type TextStreamResult struct {
	// Events is a channel that receives stream events.
	// The channel is closed when the stream ends.
	Events <-chan StreamEvent
}

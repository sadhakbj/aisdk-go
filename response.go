package aisdk

// FinishReason indicates why the model stopped generating.
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishLength        FinishReason = "length"
	FinishContentFilter FinishReason = "content_filter"
	FinishError         FinishReason = "error"
	FinishUnknown       FinishReason = "unknown"
)

// Usage contains token usage information for a request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Add adds two Usage values together.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		PromptTokens:     u.PromptTokens + other.PromptTokens,
		CompletionTokens: u.CompletionTokens + other.CompletionTokens,
		TotalTokens:      u.TotalTokens + other.TotalTokens,
	}
}

// Step represents one step in a multi-step agent execution.
type Step struct {
	Text         string           `json:"text,omitempty"`
	ToolCalls    []ToolCallData   `json:"tool_calls,omitempty"`
	ToolResults  []ToolResultData `json:"tool_results,omitempty"`
	FinishReason FinishReason     `json:"finish_reason"`
	Usage        Usage            `json:"usage"`
}

// Response is the result of a Prompt() call.
type Response struct {
	Text           string           `json:"text"`
	Messages       []Message        `json:"messages,omitempty"`
	FinishReason   FinishReason     `json:"finish_reason"`
	Usage          Usage            `json:"usage"`
	Steps          []Step           `json:"steps,omitempty"`
	ToolCalls      []ToolCallData   `json:"tool_calls,omitempty"`
	ToolResults    []ToolResultData `json:"tool_results,omitempty"`
	Provider       string           `json:"provider"`
	Model          string           `json:"model"`
	ConversationID string           `json:"conversation_id,omitempty"`
}

// ObjectResponse is the result of a structured output call.
type ObjectResponse[T any] struct {
	Object       T            `json:"object"`
	Text         string       `json:"text"`
	FinishReason FinishReason `json:"finish_reason"`
	Usage        Usage        `json:"usage"`
	Provider     string       `json:"provider"`
	Model        string       `json:"model"`
}

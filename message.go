package aisdk

import "encoding/json"

// Role represents the role of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message represents a single message in a conversation.
type Message struct {
	Role         Role             `json:"role"`
	Content      string           `json:"content,omitempty"`
	Name         string           `json:"name,omitempty"`
	ToolCalls    []ToolCallData   `json:"tool_calls,omitzero"`
	ToolCallID   string           `json:"tool_call_id,omitempty"`
	Attachments  []Attachment     `json:"attachments,omitzero"`
}

// ToolCallData represents a tool call made by the assistant.
type ToolCallData struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResultData represents the result of a tool execution.
type ToolResultData struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Result    any    `json:"result"`
	IsError   bool   `json:"is_error,omitzero"`
}

// Attachment represents a file or media attached to a message.
type Attachment struct {
	Type     string `json:"type"`     // "image", "file", "url"
	URL      string `json:"url,omitempty"`
	Path     string `json:"path,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Data     []byte `json:"data,omitzero"`
}

// --- Message constructors ---

// System creates a system message.
func System(content string) Message {
	return Message{Role: RoleSystem, Content: content}
}

// User creates a user message.
func User(content string) Message {
	return Message{Role: RoleUser, Content: content}
}

// UserWithAttachments creates a user message with attachments.
func UserWithAttachments(content string, attachments ...Attachment) Message {
	return Message{Role: RoleUser, Content: content, Attachments: attachments}
}

// Assistant creates an assistant message.
func Assistant(content string) Message {
	return Message{Role: RoleAssistant, Content: content}
}

// AssistantWithToolCalls creates an assistant message that includes tool calls.
func AssistantWithToolCalls(content string, toolCalls ...ToolCallData) Message {
	return Message{Role: RoleAssistant, Content: content, ToolCalls: toolCalls}
}

// ToolResult creates a tool result message.
func ToolResult(toolCallID string, result any) Message {
	b, _ := json.Marshal(result)
	return Message{Role: RoleTool, Content: string(b), ToolCallID: toolCallID}
}

// ToolErrorResult creates a tool result message indicating an error.
func ToolErrorResult(toolCallID string, err error) Message {
	return Message{Role: RoleTool, Content: err.Error(), ToolCallID: toolCallID}
}

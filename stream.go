package aisdk

import "encoding/json"

// StreamEvent is the interface for all streaming events.
// The unexported marker method prevents external implementations.
type StreamEvent interface {
	eventType() string
}

// --- Stream event types ---

// StreamStart is emitted when a stream begins.
type StreamStart struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (e *StreamStart) eventType() string { return "stream_start" }

// StreamEnd is emitted when a stream completes.
type StreamEnd struct {
	FinishReason FinishReason `json:"finish_reason"`
	Usage        Usage        `json:"usage"`
}

func (e *StreamEnd) eventType() string { return "stream_end" }

// TextDelta is emitted for each text chunk during streaming.
type TextDelta struct {
	Text string `json:"text"`
}

func (e *TextDelta) eventType() string { return "text_delta" }

// ReasoningDelta is emitted for reasoning/thinking content (e.g. Claude extended thinking).
type ReasoningDelta struct {
	Text string `json:"text"`
}

func (e *ReasoningDelta) eventType() string { return "reasoning_delta" }

// ToolCallEvent is emitted when the model invokes a tool.
type ToolCallEvent struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

func (e *ToolCallEvent) eventType() string { return "tool_call" }

// ToolResultEvent is emitted after a tool has been executed.
type ToolResultEvent struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Result any    `json:"result"`
	Err    error  `json:"-"`
}

func (e *ToolResultEvent) eventType() string { return "tool_result" }

// ErrorEvent is emitted when a recoverable error occurs during streaming.
type ErrorEvent struct {
	Err         error `json:"-"`
	Recoverable bool  `json:"recoverable"`
}

func (e *ErrorEvent) eventType() string { return "error" }

// Stream wraps a streaming response and provides a channel-based API.
type Stream struct {
	events   <-chan StreamEvent
	done     chan struct{}
	response *Response

	// collected during iteration
	text         string
	usage        Usage
	finishReason FinishReason
	provider     string
	model        string
	toolCalls    []ToolCallData
	toolResults  []ToolResultData
}

// NewStream creates a Stream from a channel of events.
func NewStream(events <-chan StreamEvent) *Stream {
	return &Stream{
		events: events,
		done:   make(chan struct{}),
	}
}

// Events returns a channel that yields StreamEvents.
// Iterate with: for event := range stream.Events() { ... }
func (s *Stream) Events() <-chan StreamEvent {
	ch := make(chan StreamEvent)
	go func() {
		defer close(ch)
		defer close(s.done)
		for event := range s.events {
			// Collect data for the final Response
			switch e := event.(type) {
			case *StreamStart:
				s.provider = e.Provider
				s.model = e.Model
			case *TextDelta:
				s.text += e.Text
			case *ToolCallEvent:
				s.toolCalls = append(s.toolCalls, ToolCallData{
					ID:        e.ID,
					Name:      e.Name,
					Arguments: e.Args,
				})
			case *ToolResultEvent:
				s.toolResults = append(s.toolResults, ToolResultData{
					ID:      e.ID,
					Name:    e.Name,
					Result:  e.Result,
					IsError: e.Err != nil,
				})
			case *StreamEnd:
				s.finishReason = e.FinishReason
				s.usage = e.Usage
			}
			ch <- event
		}
	}()
	return ch
}

// Response returns the aggregated Response after the stream has completed.
// This blocks until the stream is fully consumed.
func (s *Stream) Response() *Response {
	<-s.done
	if s.response != nil {
		return s.response
	}
	s.response = &Response{
		Text:         s.text,
		FinishReason: s.finishReason,
		Usage:        s.usage,
		Provider:     s.provider,
		Model:        s.model,
		ToolCalls:    s.toolCalls,
		ToolResults:  s.toolResults,
	}
	return s.response
}

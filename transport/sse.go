// Package transport provides HTTP handlers for serving AI responses.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	aisdk "github.com/sadhakbj/aisdk-go"
)

// SSEHandler returns an http.Handler that streams agent responses as
// standard Server-Sent Events.
func SSEHandler(agent aisdk.Agent) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			Prompt   string         `json:"prompt"`
			Messages []aisdk.Message `json:"messages,omitzero"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		prompt := body.Prompt
		if prompt == "" && len(body.Messages) > 0 {
			// Use the last user message as the prompt
			for i := len(body.Messages) - 1; i >= 0; i-- {
				if body.Messages[i].Role == aisdk.RoleUser {
					prompt = body.Messages[i].Content
					break
				}
			}
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		stream, err := agent.Stream(r.Context(), prompt)
		if err != nil {
			writeSSEError(w, flusher, err)
			return
		}

		for event := range stream.Events() {
			data := marshalEvent(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
}

// SSEHandlerWithApp creates an SSE handler using direct text params instead of an agent.
func SSEHandlerWithApp(app *aisdk.App, model string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			Prompt   string         `json:"prompt"`
			Messages []aisdk.Message `json:"messages,omitzero"`
			System   string         `json:"system,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		stream, err := app.StreamText(r.Context(), aisdk.TextParams{
			Model:    model,
			System:   body.System,
			Prompt:   body.Prompt,
			Messages: body.Messages,
		})
		if err != nil {
			writeSSEError(w, flusher, err)
			return
		}

		for event := range stream.Events() {
			data := marshalEvent(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
}

func writeSSEError(w io.Writer, flusher http.Flusher, err error) {
	data, _ := json.Marshal(map[string]any{
		"type":  "error",
		"error": err.Error(),
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func marshalEvent(event aisdk.StreamEvent) string {
	// Wrap the event with a "type" field so the client can distinguish events.
	wrapper := map[string]any{}

	switch e := event.(type) {
	case *aisdk.StreamStart:
		wrapper["type"] = "start"
		wrapper["provider"] = e.Provider
		wrapper["model"] = e.Model
	case *aisdk.TextDelta:
		wrapper["type"] = "text"
		wrapper["text"] = e.Text
	case *aisdk.ReasoningDelta:
		wrapper["type"] = "reasoning"
		wrapper["text"] = e.Text
	case *aisdk.ToolCallEvent:
		wrapper["type"] = "tool_call"
		wrapper["id"] = e.ID
		wrapper["name"] = e.Name
		wrapper["args"] = string(e.Args)
	case *aisdk.ToolResultEvent:
		wrapper["type"] = "tool_result"
		wrapper["id"] = e.ID
		wrapper["result"] = e.Result
	case *aisdk.StreamEnd:
		wrapper["type"] = "end"
		wrapper["finish_reason"] = string(e.FinishReason)
		wrapper["usage"] = e.Usage
	case *aisdk.ErrorEvent:
		wrapper["type"] = "error"
		wrapper["error"] = e.Err.Error()
	default:
		data, _ := json.Marshal(event)
		return string(data)
	}

	data, _ := json.Marshal(wrapper)
	return string(data)
}

// readPromptFromRequest is used by both handlers to extract prompt from request.
func readPromptFromRequest(ctx context.Context, r io.Reader) (string, []aisdk.Message, error) {
	var body struct {
		Prompt   string         `json:"prompt"`
		Messages []aisdk.Message `json:"messages,omitzero"`
	}
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return "", nil, err
	}
	return body.Prompt, body.Messages, nil
}

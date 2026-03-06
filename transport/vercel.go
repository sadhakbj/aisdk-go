package transport

import (
	"encoding/json"
	"fmt"
	"net/http"

	aisdk "github.com/sadhakbj/aisdk-go"
)

// VercelHandler returns an http.Handler compatible with Vercel AI SDK's
// Data Stream Protocol. This works with Next.js useChat() and useCompletion() hooks.
//
// Event mapping:
//
//	StreamStart     -> {type: "start", messageId: "..."}
//	TextDelta       -> {type: "text-delta", textDelta: "..."}
//	ReasoningDelta  -> {type: "reasoning", textDelta: "..."}
//	ToolCallEvent   -> {type: "tool-call", toolCallId, toolName, args}
//	ToolResultEvent -> {type: "tool-result", toolCallId, result}
//	StreamEnd       -> {type: "finish", finishReason, usage}
//	ErrorEvent      -> {type: "error", errorText: "..."}
func VercelHandler(agent aisdk.Agent) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			Messages []vercelMessage `json:"messages"`
			System   string          `json:"system,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Extract the last user message as the prompt
		var prompt string
		for i := len(body.Messages) - 1; i >= 0; i-- {
			if body.Messages[i].Role == "user" {
				prompt = body.Messages[i].Content
				break
			}
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("x-vercel-ai-data-stream", "v1")

		stream, err := agent.Stream(r.Context(), prompt)
		if err != nil {
			writeVercelError(w, flusher, err)
			return
		}

		messageID := "msg-1"
		for event := range stream.Events() {
			var vercelEvent map[string]any

			switch e := event.(type) {
			case *aisdk.StreamStart:
				vercelEvent = map[string]any{
					"type":      "start",
					"messageId": messageID,
				}
			case *aisdk.TextDelta:
				vercelEvent = map[string]any{
					"type":      "text-delta",
					"textDelta": e.Text,
				}
			case *aisdk.ReasoningDelta:
				vercelEvent = map[string]any{
					"type":      "reasoning",
					"textDelta": e.Text,
				}
			case *aisdk.ToolCallEvent:
				vercelEvent = map[string]any{
					"type":       "tool-call",
					"toolCallId": e.ID,
					"toolName":   e.Name,
					"args":       string(e.Args),
				}
			case *aisdk.ToolResultEvent:
				resultJSON, _ := json.Marshal(e.Result)
				vercelEvent = map[string]any{
					"type":       "tool-result",
					"toolCallId": e.ID,
					"result":     string(resultJSON),
				}
			case *aisdk.StreamEnd:
				vercelEvent = map[string]any{
					"type":         "finish",
					"finishReason": string(e.FinishReason),
					"usage": map[string]any{
						"promptTokens":     e.Usage.PromptTokens,
						"completionTokens": e.Usage.CompletionTokens,
					},
				}
			case *aisdk.ErrorEvent:
				vercelEvent = map[string]any{
					"type":      "error",
					"errorText": e.Err.Error(),
				}
			default:
				continue
			}

			data, _ := json.Marshal(vercelEvent)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
}

type vercelMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func writeVercelError(w http.ResponseWriter, flusher http.Flusher, err error) {
	data, _ := json.Marshal(map[string]any{
		"type":      "error",
		"errorText": err.Error(),
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

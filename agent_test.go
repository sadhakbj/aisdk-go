package aisdk_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sadhakbj/aisdk-go"
	aisdktest "github.com/sadhakbj/aisdk-go/testing"
)

// recordingTool is a minimal client-side tool that records every call.
type recordingTool struct {
	name        string
	description string
	calls       atomic.Int32
	lastArgs    atomic.Value // string
	respond     func(args json.RawMessage) (any, error)
}

func newRecordingTool(name string, respond func(json.RawMessage) (any, error)) *recordingTool {
	return &recordingTool{name: name, description: name + " test tool", respond: respond}
}

type recordingToolParams struct {
	City string `json:"city"`
}

func (t *recordingTool) Name() string        { return t.name }
func (t *recordingTool) Description() string { return t.description }
func (t *recordingTool) Parameters() any     { return recordingToolParams{} }
func (t *recordingTool) Execute(_ context.Context, args json.RawMessage) (any, error) {
	t.calls.Add(1)
	t.lastArgs.Store(string(args))
	if t.respond != nil {
		return t.respond(args)
	}
	return map[string]any{"ok": true}, nil
}

func TestAgentToolCallSingleStep(t *testing.T) {
	fake := aisdktest.NewFakeApp(t)

	tool := newRecordingTool("get_weather", func(json.RawMessage) (any, error) {
		return map[string]any{"temp": 72, "condition": "sunny"}, nil
	})

	// Step 1: model emits a tool_call.
	// Step 2: model responds with the final answer.
	fake.FakeText(
		&aisdk.TextResult{
			ToolCalls: []aisdk.ToolCallData{{
				ID: "call-1", Name: "get_weather",
				Arguments: json.RawMessage(`{"city":"Tokyo"}`),
			}},
			FinishReason: aisdk.FinishToolCalls,
		},
		&aisdk.TextResult{
			Content:      "Tokyo is 72°F and sunny.",
			FinishReason: aisdk.FinishStop,
		},
	)

	agent, err := aisdk.Quick(aisdk.AgentConfig{
		Tools:    []aisdk.Tool{tool},
		MaxSteps: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := agent.Prompt(t.Context(), "What's the weather in Tokyo?")
	if err != nil {
		t.Fatal(err)
	}

	if tool.calls.Load() != 1 {
		t.Errorf("expected tool to be called once, got %d", tool.calls.Load())
	}
	if got := tool.lastArgs.Load(); got != `{"city":"Tokyo"}` {
		t.Errorf("unexpected tool args: %v", got)
	}
	if resp.Text != "Tokyo is 72°F and sunny." {
		t.Errorf("unexpected response text: %q", resp.Text)
	}
	if len(resp.Steps) != 2 {
		t.Errorf("expected 2 steps (tool_call + final), got %d", len(resp.Steps))
	}
	if len(resp.ToolCalls) != 1 {
		t.Errorf("expected 1 recorded tool call, got %d", len(resp.ToolCalls))
	}
	if len(resp.ToolResults) != 1 {
		t.Errorf("expected 1 recorded tool result, got %d", len(resp.ToolResults))
	}
	if resp.ToolResults[0].IsError {
		t.Error("tool result should not be an error")
	}
}

func TestAgentToolCallMultiStep(t *testing.T) {
	// Model calls tool twice (different cities) before answering.
	fake := aisdktest.NewFakeApp(t)
	tool := newRecordingTool("get_weather", nil)

	fake.FakeText(
		&aisdk.TextResult{
			ToolCalls: []aisdk.ToolCallData{{
				ID: "1", Name: "get_weather",
				Arguments: json.RawMessage(`{"city":"Tokyo"}`),
			}},
			FinishReason: aisdk.FinishToolCalls,
		},
		&aisdk.TextResult{
			ToolCalls: []aisdk.ToolCallData{{
				ID: "2", Name: "get_weather",
				Arguments: json.RawMessage(`{"city":"Paris"}`),
			}},
			FinishReason: aisdk.FinishToolCalls,
		},
		&aisdk.TextResult{
			Content:      "Both are sunny.",
			FinishReason: aisdk.FinishStop,
		},
	)

	agent, err := aisdk.Quick(aisdk.AgentConfig{
		Tools:    []aisdk.Tool{tool},
		MaxSteps: 5,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := agent.Prompt(t.Context(), "Compare Tokyo and Paris.")
	if err != nil {
		t.Fatal(err)
	}

	if tool.calls.Load() != 2 {
		t.Errorf("expected 2 tool calls, got %d", tool.calls.Load())
	}
	if len(resp.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(resp.Steps))
	}
	if resp.Text != "Both are sunny." {
		t.Errorf("unexpected response text: %q", resp.Text)
	}
}

func TestAgentRespectsMaxSteps(t *testing.T) {
	// Model never stops calling the tool. MaxSteps=2 should cap the loop and
	// return FinishLength.
	fake := aisdktest.NewFakeApp(t)
	tool := newRecordingTool("loop_tool", nil)

	for range 5 {
		fake.FakeText(&aisdk.TextResult{
			ToolCalls: []aisdk.ToolCallData{{
				ID: "loop", Name: "loop_tool",
				Arguments: json.RawMessage(`{}`),
			}},
			FinishReason: aisdk.FinishToolCalls,
		})
	}

	agent, err := aisdk.Quick(aisdk.AgentConfig{
		Tools:    []aisdk.Tool{tool},
		MaxSteps: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := agent.Prompt(t.Context(), "spin forever")
	if err != nil {
		t.Fatal(err)
	}

	if resp.FinishReason != aisdk.FinishLength {
		t.Errorf("expected FinishLength, got %v", resp.FinishReason)
	}
	if tool.calls.Load() != 2 {
		t.Errorf("expected exactly MaxSteps=2 tool calls, got %d", tool.calls.Load())
	}
}

func TestAgentUnknownToolProducesErrorResult(t *testing.T) {
	fake := aisdktest.NewFakeApp(t)

	fake.FakeText(
		&aisdk.TextResult{
			ToolCalls: []aisdk.ToolCallData{{
				ID: "x", Name: "nonexistent_tool",
				Arguments: json.RawMessage(`{}`),
			}},
			FinishReason: aisdk.FinishToolCalls,
		},
		&aisdk.TextResult{
			Content:      "Sorry, that tool isn't available.",
			FinishReason: aisdk.FinishStop,
		},
	)

	// Register a real tool, but the model calls a different name.
	registered := newRecordingTool("known_tool", nil)
	agent, err := aisdk.Quick(aisdk.AgentConfig{
		Tools:    []aisdk.Tool{registered},
		MaxSteps: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := agent.Prompt(t.Context(), "do something")
	if err != nil {
		t.Fatal(err)
	}

	if registered.calls.Load() != 0 {
		t.Errorf("known tool should not have been invoked, got %d calls", registered.calls.Load())
	}
	if len(resp.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result entry, got %d", len(resp.ToolResults))
	}
	got := resp.ToolResults[0]
	if !got.IsError {
		t.Error("expected the missing-tool result to be flagged IsError")
	}
	if msg, _ := got.Result.(string); !strings.Contains(msg, `"nonexistent_tool"`) {
		t.Errorf("expected error to mention tool name, got %v", got.Result)
	}
}

func TestAgentToolExecuteErrorIsRecorded(t *testing.T) {
	fake := aisdktest.NewFakeApp(t)

	failing := newRecordingTool("flaky", func(json.RawMessage) (any, error) {
		return nil, errors.New("upstream is down")
	})

	fake.FakeText(
		&aisdk.TextResult{
			ToolCalls: []aisdk.ToolCallData{{
				ID: "f", Name: "flaky",
				Arguments: json.RawMessage(`{}`),
			}},
			FinishReason: aisdk.FinishToolCalls,
		},
		&aisdk.TextResult{
			Content:      "Tool failed; falling back.",
			FinishReason: aisdk.FinishStop,
		},
	)

	agent, err := aisdk.Quick(aisdk.AgentConfig{
		Tools:    []aisdk.Tool{failing},
		MaxSteps: 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := agent.Prompt(t.Context(), "try the flaky thing")
	if err != nil {
		t.Fatal(err)
	}

	if failing.calls.Load() != 1 {
		t.Errorf("expected the failing tool to be called once, got %d", failing.calls.Load())
	}
	if len(resp.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(resp.ToolResults))
	}
	if !resp.ToolResults[0].IsError {
		t.Error("expected tool result to be flagged IsError")
	}
	if msg, _ := resp.ToolResults[0].Result.(string); msg != "upstream is down" {
		t.Errorf("expected exec error message, got %v", resp.ToolResults[0].Result)
	}
	if resp.Text != "Tool failed; falling back." {
		t.Errorf("expected the model's follow-up text, got %q", resp.Text)
	}
}

func TestAgentBuiltinToolNotInvokedLocally(t *testing.T) {
	// WebSearch is a BuiltinTool; even if it's in the Tools slice alongside
	// client tools, its execution path is server-side. The agent must never
	// try to "execute" it locally.
	fake := aisdktest.NewFakeApp(t)
	fake.FakeText(aisdktest.Response("All good."))

	tool := newRecordingTool("client_tool", nil)
	agent, err := aisdk.Quick(aisdk.AgentConfig{
		Tools:    []aisdk.Tool{&aisdk.WebSearch{}, tool},
		MaxSteps: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := agent.Prompt(t.Context(), "hi")
	if err != nil {
		t.Fatal(err)
	}

	if tool.calls.Load() != 0 {
		t.Errorf("client tool shouldn't have been invoked, got %d", tool.calls.Load())
	}
	if resp.Text != "All good." {
		t.Errorf("unexpected text: %q", resp.Text)
	}
}

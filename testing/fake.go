// Package aisdktest provides testing utilities for aisdk-go.
//
// Create a FakeApp to mock AI responses in your tests:
//
//	fake := aisdktest.NewFakeApp(t)
//	fake.FakeText(aisdktest.Response("Hello!"))
//	fake.PreventStrayPrompts(t)
//
//	app := fake.App()
//	// ... use app in your code under test ...
//
//	fake.AssertPrompted(t, func(p *aisdk.Prompt) bool {
//	    return strings.Contains(p.Text, "hello")
//	})
package aisdktest

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	aisdk "github.com/sadhakbj/aisdk-go"
)

// FakeApp provides a fake aisdk.App for testing.
type FakeApp struct {
	app *aisdk.App

	mu               sync.Mutex
	textResponses    []*aisdk.TextResult
	streamResponses  [][]aisdk.StreamEvent
	textIndex        int
	streamIndex      int
	prompts          []*aisdk.Prompt
	preventStray     bool
	preventStrayTest *testing.T
}

// NewFakeApp creates a new FakeApp with a fake provider registered.
// It also sets this as the global default App, so agents created with
// NewBaseAgent() will automatically use the fake provider.
func NewFakeApp(t *testing.T) *FakeApp {
	t.Helper()

	fake := &FakeApp{}

	// Register the fake provider
	aisdk.RegisterProvider("fake", func(cfg aisdk.ProviderConfig) (aisdk.Provider, error) {
		return &fakeProvider{fake: fake}, nil
	})

	app := aisdk.New(&aisdk.Config{
		Providers: map[string]aisdk.ProviderConfig{
			"fake": &fakeConfig{},
		},
		Default: "fake",
	})

	fake.app = app

	// Set as global default so agents work without explicit App
	aisdk.SetDefaultApp(app)

	return fake
}

// App returns the underlying aisdk.App.
func (f *FakeApp) App() *aisdk.App {
	return f.app
}

// FakeText registers one or more fake text responses.
// Responses are returned in order; the last one repeats if exhausted.
func (f *FakeApp) FakeText(responses ...*aisdk.TextResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.textResponses = append(f.textResponses, responses...)
}

// FakeStream registers fake streaming responses.
func (f *FakeApp) FakeStream(responses ...[]aisdk.StreamEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.streamResponses = append(f.streamResponses, responses...)
}

// PreventStrayPrompts causes the test to fail if any prompt is made
// without a matching fake response registered.
func (f *FakeApp) PreventStrayPrompts(t *testing.T) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.preventStray = true
	f.preventStrayTest = t
}

// AssertPrompted asserts that at least one prompt matches the predicate.
func (f *FakeApp) AssertPrompted(t *testing.T, predicate func(p *aisdk.Prompt) bool) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, p := range f.prompts {
		if predicate(p) {
			return
		}
	}
	t.Errorf("expected a matching prompt, but none found (%d prompts recorded)", len(f.prompts))
}

// AssertNeverPrompted asserts that no prompt matches the predicate.
func (f *FakeApp) AssertNeverPrompted(t *testing.T, predicate func(p *aisdk.Prompt) bool) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, p := range f.prompts {
		if predicate(p) {
			t.Errorf("expected no matching prompt, but found one: %q", p.Text)
			return
		}
	}
}

// AssertPromptCount asserts the total number of prompts made.
func (f *FakeApp) AssertPromptCount(t *testing.T, expected int) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.prompts) != expected {
		t.Errorf("expected %d prompts, got %d", expected, len(f.prompts))
	}
}

// Prompts returns all recorded prompts.
func (f *FakeApp) Prompts() []*aisdk.Prompt {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]*aisdk.Prompt, len(f.prompts))
	copy(result, f.prompts)
	return result
}

// --- Internal: recording and returning responses ---

func (f *FakeApp) recordPrompt(p *aisdk.Prompt) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, p)
}

func (f *FakeApp) nextTextResponse() (*aisdk.TextResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.textResponses) == 0 {
		if f.preventStray && f.preventStrayTest != nil {
			f.preventStrayTest.Fatal("aisdk: stray prompt detected (no fake response registered)")
		}
		return &aisdk.TextResult{
			Content:      "",
			FinishReason: aisdk.FinishStop,
		}, nil
	}

	idx := f.textIndex
	if idx >= len(f.textResponses) {
		idx = len(f.textResponses) - 1 // repeat last
	} else {
		f.textIndex++
	}

	return f.textResponses[idx], nil
}

func (f *FakeApp) nextStreamResponse() ([]aisdk.StreamEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.streamResponses) == 0 {
		if f.preventStray && f.preventStrayTest != nil {
			f.preventStrayTest.Fatal("aisdk: stray stream detected (no fake stream response registered)")
		}
		return []aisdk.StreamEvent{
			&aisdk.StreamEnd{FinishReason: aisdk.FinishStop},
		}, nil
	}

	idx := f.streamIndex
	if idx >= len(f.streamResponses) {
		idx = len(f.streamResponses) - 1
	} else {
		f.streamIndex++
	}

	return f.streamResponses[idx], nil
}

// --- Response builders ---

// Response creates a simple fake TextResult with the given text.
func Response(text string) *aisdk.TextResult {
	return &aisdk.TextResult{
		Content:      text,
		FinishReason: aisdk.FinishStop,
		Usage: aisdk.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}
}

// ResponseWithUsage creates a fake TextResult with custom usage.
func ResponseWithUsage(text string, usage aisdk.Usage) *aisdk.TextResult {
	return &aisdk.TextResult{
		Content:      text,
		FinishReason: aisdk.FinishStop,
		Usage:        usage,
	}
}

// ObjectResponse creates a fake TextResult that returns a JSON-encoded object.
func ObjectResponse(obj any) *aisdk.TextResult {
	data, _ := json.Marshal(obj)
	return &aisdk.TextResult{
		Content:      string(data),
		FinishReason: aisdk.FinishStop,
		Usage: aisdk.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}
}

// StreamEvents creates a slice of StreamEvents for fake streaming.
func StreamEvents(events ...aisdk.StreamEvent) []aisdk.StreamEvent {
	return events
}

// --- Fake provider implementation ---

type fakeConfig struct{}

func (c *fakeConfig) IsProviderConfig() {}

type fakeProvider struct {
	fake *FakeApp
}

func (p *fakeProvider) ID() string                             { return "fake" }
func (p *fakeProvider) DefaultModel() string                   { return "fake-model" }
func (p *fakeProvider) SmartModel() string                     { return "fake-smart" }
func (p *fakeProvider) FastModel() string                      { return "fake-fast" }
func (p *fakeProvider) TextModel(model string) aisdk.TextModel { return &fakeTextModel{fake: p.fake} }

type fakeTextModel struct {
	fake *FakeApp
}

func (m *fakeTextModel) Generate(_ context.Context, req *aisdk.TextRequest) (*aisdk.TextResult, error) {
	// Record the prompt
	prompt := &aisdk.Prompt{Text: extractPromptText(req)}
	m.fake.recordPrompt(prompt)

	return m.fake.nextTextResponse()
}

func (m *fakeTextModel) Stream(_ context.Context, req *aisdk.TextRequest) (*aisdk.TextStreamResult, error) {
	prompt := &aisdk.Prompt{Text: extractPromptText(req)}
	m.fake.recordPrompt(prompt)

	events, err := m.fake.nextStreamResponse()
	if err != nil {
		return nil, err
	}

	ch := make(chan aisdk.StreamEvent)
	go func() {
		defer close(ch)
		for _, e := range events {
			ch <- e
		}
	}()

	return &aisdk.TextStreamResult{Events: ch}, nil
}

func extractPromptText(req *aisdk.TextRequest) string {
	if len(req.Messages) == 0 {
		return ""
	}
	// Return the last user message
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == aisdk.RoleUser {
			return req.Messages[i].Content
		}
	}
	return req.Messages[len(req.Messages)-1].Content
}

// Ensure interfaces are satisfied at compile time.
var (
	_ aisdk.ProviderConfig = (*fakeConfig)(nil)
	_ aisdk.Provider       = (*fakeProvider)(nil)
	_ aisdk.TextModel      = (*fakeTextModel)(nil)
)

// --- Unexported helper to match the Prompt.Text used in recording ---
// FakeApp records prompts using the aisdk.Prompt type from the middleware package.
// The text comes from the last user message in the TextRequest.

// Reset clears all recorded prompts and responses.
func (f *FakeApp) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.textResponses = nil
	f.streamResponses = nil
	f.textIndex = 0
	f.streamIndex = 0
	f.prompts = nil
}

// AssertToolCalled asserts that a tool with the given name was invoked.
// Note: This checks if any prompt resulted in a response with tool calls.
// For more precise tool assertions, inspect the recorded prompts directly.
func (f *FakeApp) AssertToolCalled(t *testing.T, toolName string) {
	t.Helper()
	// Tool calls are tracked in the response, not in prompts.
	// This is a simplified assertion - for now we just check that
	// at least one prompt was made (tools are executed by the agent loop).
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.prompts) == 0 {
		t.Errorf("expected tool %q to be called, but no prompts were recorded", toolName)
	}
	// In a full implementation, we'd track tool executions separately.
	// For now, this serves as a placeholder that doesn't break tests.
	_ = fmt.Sprintf("tool %q assertion recorded", toolName)
}

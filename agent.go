package aisdk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Agent is the core contract for an AI agent.
type Agent interface {
	// Instructions returns the system prompt for this agent.
	Instructions() string

	// Prompt sends a synchronous prompt and returns the response.
	Prompt(ctx context.Context, prompt string, opts ...PromptOption) (*Response, error)

	// Stream sends a streaming prompt and returns a Stream.
	Stream(ctx context.Context, prompt string, opts ...PromptOption) (*Stream, error)
}

// AgentHooks is optionally implemented by agents to hook into the lifecycle.
type AgentHooks interface {
	BeforePrompt(ctx context.Context, p *Prompt)
	AfterPrompt(ctx context.Context, r *Response)
}

// AgentConfig holds configuration for creating a BaseAgent.
type AgentConfig struct {
	Provider     string
	Model        string
	Instructions string
	Tools        []Tool
	MaxSteps     int
	Temperature  float64
	MaxTokens    int
	Timeout      time.Duration
	Middleware   []Middleware
}

// BaseAgent provides the default agent implementation.
// Embed this in your agent structs and override hooks as needed.
type BaseAgent struct {
	app    *App
	config AgentConfig

	// Conversation state
	userID         string
	conversationID string
}

// NewBaseAgent creates a new BaseAgent using the global default App.
// This is the recommended way to create agents. Configure the App first
// with aisdk.Configure(), then just define your agents:
//
//	type SalesCoach struct { aisdk.BaseAgent }
//	func NewSalesCoach() *SalesCoach {
//	    return &SalesCoach{BaseAgent: aisdk.NewBaseAgent(aisdk.AgentConfig{...})}
//	}
func NewBaseAgent(config AgentConfig) BaseAgent {
	if config.MaxSteps <= 0 {
		config.MaxSteps = 1
	}
	return BaseAgent{
		app:    nil, // resolved lazily from DefaultApp()
		config: config,
	}
}

// NewBaseAgentWithApp creates a new BaseAgent with an explicit App.
// Use this when you need a different App than the global default (e.g. in tests).
func NewBaseAgentWithApp(app *App, config AgentConfig) BaseAgent {
	if config.MaxSteps <= 0 {
		config.MaxSteps = 1
	}
	return BaseAgent{
		app:    app,
		config: config,
	}
}

// App returns the associated App instance.
// If no explicit App was set, returns the global default App.
func (a *BaseAgent) App() *App {
	if a.app != nil {
		return a.app
	}
	return DefaultApp()
}

// SetApp explicitly sets the App for this agent.
func (a *BaseAgent) SetApp(app *App) {
	a.app = app
}

// Instructions returns the agent's system prompt.
func (a *BaseAgent) Instructions() string {
	return a.config.Instructions
}

// ForUser starts a new conversation for the given user.
func (a *BaseAgent) ForUser(userID string) {
	a.userID = userID
	a.conversationID = ""
}

// Continue resumes an existing conversation.
func (a *BaseAgent) Continue(conversationID string, userID string) {
	a.conversationID = conversationID
	a.userID = userID
}

// ContinueLast resumes the user's most recent conversation.
func (a *BaseAgent) ContinueLast(userID string) {
	a.userID = userID
	// conversationID will be resolved lazily in buildMessages
	a.conversationID = "__latest__"
}

// Prompt sends a synchronous prompt to the model.
func (a *BaseAgent) Prompt(ctx context.Context, prompt string, opts ...PromptOption) (*Response, error) {
	o := applyPromptOptions(opts)

	// Resolve provider and model
	providerName, modelName, err := a.resolveProviderModel(o)
	if err != nil {
		return nil, err
	}

	textModel, err := a.getTextModel(providerName, modelName)
	if err != nil {
		return nil, err
	}

	// Apply timeout
	timeout := a.config.Timeout
	if o.timeout != nil {
		timeout = *o.timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build the prompt
	p, err := a.buildPrompt(ctx, prompt)
	if err != nil {
		return nil, err
	}

	// Call hooks if the concrete agent implements them
	if hooks, ok := a.findHooks(); ok {
		hooks.BeforePrompt(ctx, p)
	}

	// Build the core handler
	handler := func(ctx context.Context, p *Prompt) (*Response, error) {
		return a.executePrompt(ctx, textModel, providerName, modelName, p, o)
	}

	// Run through middleware
	resp, err := runMiddleware(ctx, p, a.config.Middleware, handler)
	if err != nil {
		return nil, err
	}

	// After hook
	if hooks, ok := a.findHooks(); ok {
		hooks.AfterPrompt(ctx, resp)
	}

	// Store conversation messages
	if a.userID != "" {
		a.storeConversation(ctx, prompt, resp)
	}

	resp.Provider = providerName
	resp.Model = modelName
	resp.ConversationID = a.conversationID

	return resp, nil
}

// Stream sends a streaming prompt to the model.
func (a *BaseAgent) Stream(ctx context.Context, prompt string, opts ...PromptOption) (*Stream, error) {
	o := applyPromptOptions(opts)

	providerName, modelName, err := a.resolveProviderModel(o)
	if err != nil {
		return nil, err
	}

	textModel, err := a.getTextModel(providerName, modelName)
	if err != nil {
		return nil, err
	}

	timeout := a.config.Timeout
	if o.timeout != nil {
		timeout = *o.timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		// cancel will be called when stream is done
		_ = cancel
	}

	p, err := a.buildPrompt(ctx, prompt)
	if err != nil {
		return nil, err
	}

	if hooks, ok := a.findHooks(); ok {
		hooks.BeforePrompt(ctx, p)
	}

	// Build messages
	messages := a.buildMessages(p)

	req := &TextRequest{
		Model:    modelName,
		System:   a.config.Instructions,
		Messages: messages,
	}

	if len(a.config.Tools) > 0 {
		req.Tools = ToolsToDefinitions(a.config.Tools)
	}

	a.applyOptions(req, o)

	result, err := textModel.Stream(ctx, req)
	if err != nil {
		return nil, err
	}

	// Wrap the raw events with provider/model info
	wrappedCh := make(chan StreamEvent)
	go func() {
		defer close(wrappedCh)
		wrappedCh <- &StreamStart{Provider: providerName, Model: modelName}
		for event := range result.Events {
			wrappedCh <- event
		}
	}()

	return NewStream(wrappedCh), nil
}

// --- Internal helpers ---

func (a *BaseAgent) resolveProviderModel(o *promptOptions) (string, string, error) {
	modelInput := a.config.Model

	// Option overrides take precedence
	if o.model != "" {
		modelInput = o.model
	}

	// Always resolve through ResolveModel — handles "smart", "fast",
	// "provider/model", aliases, empty string, everything.
	providerName, modelName, err := a.App().ResolveModel(modelInput)
	if err != nil {
		return "", "", err
	}

	// Explicit provider override (from AgentConfig or PromptOption)
	if o.provider != "" {
		providerName = o.provider
	} else if a.config.Provider != "" {
		providerName = a.config.Provider
	}

	return providerName, modelName, nil
}

func (a *BaseAgent) getTextModel(providerName, modelName string) (TextModel, error) {
	p, err := a.App().Provider(providerName)
	if err != nil {
		return nil, err
	}
	return p.TextModel(modelName), nil
}

func (a *BaseAgent) buildPrompt(ctx context.Context, prompt string) (*Prompt, error) {
	p := &Prompt{
		Text: prompt,
	}

	// Load conversation messages if applicable
	if a.conversationID != "" && a.userID != "" {
		convMsgs, err := a.loadConversationMessages(ctx)
		if err != nil {
			// Non-fatal: just start fresh
			p.Messages = nil
		} else {
			p.Messages = convMsgs
		}
	}

	return p, nil
}

func (a *BaseAgent) buildMessages(p *Prompt) []Message {
	var messages []Message
	messages = append(messages, p.Messages...)
	if p.Text != "" {
		messages = append(messages, User(p.Text))
	}
	return messages
}

func (a *BaseAgent) applyOptions(req *TextRequest, o *promptOptions) {
	temp := a.config.Temperature
	if o.temperature != nil {
		temp = *o.temperature
	}
	if temp > 0 {
		req.Temperature = &temp
	}

	maxTokens := a.config.MaxTokens
	if o.maxTokens != nil {
		maxTokens = *o.maxTokens
	}
	if maxTokens > 0 {
		req.MaxTokens = &maxTokens
	}
}

func (a *BaseAgent) executePrompt(ctx context.Context, textModel TextModel, providerName, modelName string, p *Prompt, o *promptOptions) (*Response, error) {
	messages := a.buildMessages(p)

	var allSteps []Step
	var allToolCalls []ToolCallData
	var allToolResults []ToolResultData
	var totalUsage Usage

	maxSteps := a.config.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 1
	}

	for step := 0; step < maxSteps; step++ {
		req := &TextRequest{
			Model:    modelName,
			System:   a.config.Instructions,
			Messages: messages,
		}

		if len(a.config.Tools) > 0 {
			req.Tools = ToolsToDefinitions(a.config.Tools)
		}

		a.applyOptions(req, o)

		result, err := textModel.Generate(ctx, req)
		if err != nil {
			return nil, err
		}

		totalUsage = totalUsage.Add(result.Usage)

		stepData := Step{
			Text:         result.Content,
			ToolCalls:    result.ToolCalls,
			FinishReason: result.FinishReason,
			Usage:        result.Usage,
		}

		// If no tool calls, we're done
		if len(result.ToolCalls) == 0 || len(a.config.Tools) == 0 {
			allSteps = append(allSteps, stepData)
			return &Response{
				Text:         result.Content,
				FinishReason: result.FinishReason,
				Usage:        totalUsage,
				Steps:        allSteps,
				ToolCalls:    allToolCalls,
				ToolResults:  allToolResults,
				Messages:     messages,
			}, nil
		}

		// Execute tool calls
		allToolCalls = append(allToolCalls, result.ToolCalls...)

		// Add the assistant's tool call message
		messages = append(messages, AssistantWithToolCalls(result.Content, result.ToolCalls...))

		var stepToolResults []ToolResultData
		for _, tc := range result.ToolCalls {
			tool, found := FindTool(a.config.Tools, tc.Name)
			if !found {
				errMsg := fmt.Sprintf("tool %q not found", tc.Name)
				messages = append(messages, ToolErrorResult(tc.ID, fmt.Errorf("%s", errMsg)))
				stepToolResults = append(stepToolResults, ToolResultData{
					ID: tc.ID, Name: tc.Name, Result: errMsg, IsError: true,
				})
				continue
			}

			toolResult, err := tool.Execute(ctx, tc.Arguments)
			if err != nil {
				messages = append(messages, ToolErrorResult(tc.ID, err))
				stepToolResults = append(stepToolResults, ToolResultData{
					ID: tc.ID, Name: tc.Name, Result: err.Error(), IsError: true,
				})
			} else {
				messages = append(messages, ToolResult(tc.ID, toolResult))
				stepToolResults = append(stepToolResults, ToolResultData{
					ID: tc.ID, Name: tc.Name, Result: toolResult,
				})
			}
		}

		allToolResults = append(allToolResults, stepToolResults...)
		stepData.ToolResults = stepToolResults
		allSteps = append(allSteps, stepData)
	}

	// Exceeded max steps
	return &Response{
		Text:         "",
		FinishReason: FinishLength,
		Usage:        totalUsage,
		Steps:        allSteps,
		ToolCalls:    allToolCalls,
		ToolResults:  allToolResults,
		Messages:     messages,
	}, nil
}

func (a *BaseAgent) loadConversationMessages(ctx context.Context) ([]Message, error) {
	store := a.App().Store()

	if a.conversationID == "__latest__" {
		conv, err := store.LatestForUser(ctx, a.userID)
		if err != nil {
			return nil, err
		}
		a.conversationID = conv.ID
		return conv.Messages, nil
	}

	conv, err := store.Load(ctx, a.conversationID)
	if err != nil {
		return nil, err
	}
	return conv.Messages, nil
}

func (a *BaseAgent) storeConversation(ctx context.Context, prompt string, resp *Response) {
	store := a.App().Store()

	if a.conversationID == "" || a.conversationID == "__latest__" {
		// Create new conversation
		conv := &Conversation{
			UserID: a.userID,
			Title:  truncate(prompt, 100),
		}
		if err := store.Store(ctx, conv); err != nil {
			return
		}
		a.conversationID = conv.ID
	}

	// Append user message and assistant response
	msgs := []Message{
		User(prompt),
		Assistant(resp.Text),
	}
	_ = store.AppendMessages(ctx, a.conversationID, msgs)
}

func (a *BaseAgent) findHooks() (AgentHooks, bool) {
	// The concrete agent type (embedding BaseAgent) may implement AgentHooks.
	// Since Go doesn't have self-referential generics, we rely on the fact
	// that the concrete type's methods will be called directly.
	// This method is a no-op placeholder; hooks are called by the concrete type's
	// Prompt/Stream methods if they override them. For BaseAgent alone, no hooks.
	return nil, false
}

// --- Helpers ---

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// --- Structured output from agents ---

// PromptObject sends a prompt to an agent and returns a typed structured response.
func PromptObject[T any](ctx context.Context, agent Agent, prompt string, opts ...PromptOption) (*ObjectResponse[T], error) {
	// Add instruction for JSON output
	wrappedPrompt := prompt + "\n\nRespond with valid JSON only. Do not wrap in markdown code blocks."

	resp, err := agent.Prompt(ctx, wrappedPrompt, opts...)
	if err != nil {
		return nil, err
	}

	// Strip markdown code fences if the model wraps the JSON anyway
	text := stripJSONCodeFence(resp.Text)

	var obj T
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return nil, fmt.Errorf("aisdk: failed to parse structured output: %w", err)
	}

	return &ObjectResponse[T]{
		Object:       obj,
		Text:         text,
		FinishReason: resp.FinishReason,
		Usage:        resp.Usage,
		Provider:     resp.Provider,
		Model:        resp.Model,
	}, nil
}

// stripJSONCodeFence removes markdown code fences (```json ... ``` or ``` ... ```)
// that models sometimes wrap around JSON responses.
func stripJSONCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Remove opening fence (```json or ```)
	if idx := strings.Index(s, "\n"); idx != -1 {
		s = s[idx+1:]
	}
	// Remove closing fence
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

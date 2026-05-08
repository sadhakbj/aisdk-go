// Example: agent with tools and middleware.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/sadhakbj/aisdk-go"
	_ "github.com/sadhakbj/aisdk-go/examples/config"
)

// --- Define an agent (like: aisdk new agent WeatherAssistant) ---

type WeatherAssistant struct {
	aisdk.BaseAgent
}

func NewWeatherAssistant() *WeatherAssistant {
	w := &WeatherAssistant{}
	w.BaseAgent = aisdk.NewBaseAgent(aisdk.AgentConfig{
		Model:        "fast",
		Instructions: "You are a helpful weather assistant. Use the get_weather tool to look up weather. Be concise.",
		Tools:        []aisdk.Tool{&WeatherTool{}},
		MaxSteps:     5,
		Temperature:  0.7,
		Timeout:      30 * time.Second,
		Middleware:   []aisdk.Middleware{loggingMiddleware},
		// Hooks: embedding BaseAgent means BeforePrompt on *WeatherAssistant is not auto-discovered;
		// set AgentConfig.Hooks to the value that implements aisdk.AgentHooks.
		Hooks: w,
	})
	return w
}

// BeforePrompt injects context before every prompt.
func (w *WeatherAssistant) BeforePrompt(ctx context.Context, p *aisdk.Prompt) {
	p.Prepend(aisdk.System(fmt.Sprintf("Current time: %s", time.Now().Format(time.RFC1123))))
}

// AfterPrompt satisfies aisdk.AgentHooks (optional logging hook).
func (*WeatherAssistant) AfterPrompt(context.Context, *aisdk.Response) {}

// --- Define a tool ---

type WeatherTool struct{}

func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "Get current weather for a city" }
func (t *WeatherTool) Parameters() any {
	return struct {
		City string `json:"city" jsonschema:"required,description=City name"`
	}{}
}
func (t *WeatherTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}
	return map[string]any{
		"city":        params.City,
		"temperature": 72,
		"condition":   "sunny",
		"humidity":    45,
	}, nil
}

// --- Middleware ---

func loggingMiddleware(ctx context.Context, p *aisdk.Prompt, next aisdk.NextFunc) (*aisdk.Response, error) {
	start := time.Now()
	slog.Info("ai.prompt.start", "prompt", p.Text)

	resp, err := next(ctx, p)
	if err != nil {
		slog.Error("ai.prompt.error", "error", err, "duration", time.Since(start))
		return nil, err
	}

	slog.Info("ai.prompt.done",
		"duration", time.Since(start),
		"tokens", resp.Usage.TotalTokens,
		"steps", len(resp.Steps),
	)
	return resp, nil
}

func main() {
	ctx := context.Background()

	assistant := NewWeatherAssistant()

	result, err := assistant.Prompt(ctx, "What's the weather like in Tokyo and Paris?")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:", result.Text)
	fmt.Printf("Steps: %d\n", len(result.Steps))
	fmt.Printf("Tool calls: %d\n", len(result.ToolCalls))
	for _, tc := range result.ToolCalls {
		fmt.Printf("  - %s(%s)\n", tc.Name, string(tc.Arguments))
	}

	// Override model per-call
	result, err = assistant.Prompt(ctx, "What about London?",
		aisdk.WithModel("default"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nResponse:", result.Text)
}

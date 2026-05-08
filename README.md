# aisdk-go

[![Go Reference](https://pkg.go.dev/badge/github.com/sadhakbj/aisdk-go.svg)](https://pkg.go.dev/github.com/sadhakbj/aisdk-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/sadhakbj/aisdk-go)](https://goreportcard.com/report/github.com/sadhakbj/aisdk-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.24-007d9c)](https://go.dev/doc/devel/release)

The unified AI SDK for Go — configure once, swap providers freely, test easily.

Stop wiring provider SDKs manually. `aisdk-go` gives Go applications a single, clean interface
to OpenAI, Anthropic, and more — configure once, swap providers, test with fakes.

```
go get github.com/sadhakbj/aisdk-go
```

## Features

- **Configure once, use everywhere** — Typically just credentials in `config/ai.go`
- **Providers know their models** — `"smart"`, `"fast"`, and `"default"` resolve to each provider's tier IDs
- **Multi-provider** — OpenAI and Anthropic out of the box, extensible via `Provider` interface
- **Streaming** — Channel-based streaming with typed events
- **Structured output** — `GenerateObject[T]()` with generics for compile-time type safety
- **Tool calling** — Auto multi-step tool loop with JSON schema from struct tags
- **Provider-native web search** — `&aisdk.WebSearch{}` delegates to OpenAI Responses API or Anthropic's built-in search — no third-party key needed
- **Conversations** — Pluggable `ConversationStore` with in-memory default
- **Middleware** — Intercept/modify prompts before they reach the model
- **HTTP handlers** — SSE and Vercel AI SDK Data Stream Protocol (works with Next.js `useChat()`)
- **Automatic retry** — Exponential backoff with jitter for rate limit and overload errors
- **Testing** — `FakeApp`, sequential fake responses, `AssertPrompted`, `PreventStrayPrompts`
- **Zero framework dependency** — Works with stdlib `net/http`, Chi, Gin, Echo, Fiber

## Getting Started

### 1. Configure

Create `config/ai.go` — your single source of truth:

```go
// config/ai.go
package config

import (
    "os"

    "github.com/sadhakbj/aisdk-go"
    "github.com/sadhakbj/aisdk-go/providers/openai"
    "github.com/sadhakbj/aisdk-go/providers/anthropic"
)

func init() {
    aisdk.Configure(&aisdk.Config{
        Providers: map[string]aisdk.ProviderConfig{
            "openai":    &openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")},
            "anthropic": &anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")},
        },
        Default: "openai",
    })
}
```

Just credentials and a default. No model names — each provider knows its own models.

### 2. Use it

```go
package main

import (
    "context"
    "fmt"

    "github.com/sadhakbj/aisdk-go"
    _ "yourapp/config"  // <-- one import, done
)

func main() {
    // "smart" → resolved from the provider (OpenAI → gpt-5.4-pro)
    result, _ := aisdk.GenerateText(context.Background(), aisdk.TextParams{
        Model:  "smart",
        Prompt: "Explain goroutines in one paragraph.",
    })
    fmt.Println(result.Text)
}
```

## Built-in Model Aliases

Each provider defines its own model tiers for `"default"`, `"smart"`, and `"fast"`:

| Alias       | OpenAI           | Anthropic                      |
|-------------|------------------|--------------------------------|
| `"smart"`   | `gpt-5.4-pro`    | `claude-opus-4-7`              |
| `"fast"`    | `gpt-5.4-nano`   | `claude-haiku-4-5-20251001`    |
| `"default"` | `gpt-5.4`        | `claude-sonnet-4-6`            |

```go
// Uses default provider's smart model
aisdk.GenerateText(ctx, aisdk.TextParams{Model: "smart", Prompt: "..."})

// Uses specific provider's fast model
aisdk.GenerateText(ctx, aisdk.TextParams{Model: "anthropic/fast", Prompt: "..."})

// Or use explicit model names
aisdk.GenerateText(ctx, aisdk.TextParams{Model: "openai/gpt-5.4", Prompt: "..."})
```

## Streaming

```go
stream, _ := aisdk.StreamText(ctx, aisdk.TextParams{
    Model:  "fast",
    Prompt: "Write a poem about Go.",
})

for event := range stream.Events() {
    switch e := event.(type) {
    case *aisdk.TextDelta:
        fmt.Print(e.Text)
    case *aisdk.StreamEnd:
        fmt.Printf("\nTokens: %d\n", e.Usage.TotalTokens)
    }
}
```

## Structured Output

```go
type Recipe struct {
    Name        string   `json:"name"`
    Ingredients []string `json:"ingredients"`
    Steps       []string `json:"steps"`
}

result, _ := aisdk.GenerateObject[Recipe](ctx, aisdk.ObjectParams{
    Model:  "smart",
    Prompt: "A simple pasta recipe.",
})
fmt.Println(result.Object.Name) // type-safe, no casting
```

## Agents

Create `agents/sales_coach.go`:

```go
type SalesCoach struct {
    aisdk.BaseAgent
}

func NewSalesCoach() *SalesCoach {
    return &SalesCoach{
        BaseAgent: aisdk.NewBaseAgent(aisdk.AgentConfig{
            Model:        "smart",
            Instructions: "You are an expert sales coach.",
            Tools:        []aisdk.Tool{&CRMSearch{}, &RevenueCalc{}},
            MaxSteps:     10,
            Temperature:  0.7,
        }),
    }
}

func (s *SalesCoach) BeforePrompt(ctx context.Context, p *aisdk.Prompt) {
    p.Prepend(aisdk.System("Current quarter: Q1 2026"))
}
```

```go
coach := agents.NewSalesCoach()
result, _ := coach.Prompt(ctx, "How are my numbers this quarter?")
```

### Inline Agent

```go
agent, err := aisdk.Quick(aisdk.AgentConfig{
    Instructions: "You are a code reviewer.",
})
if err != nil { return err }

result, err := agent.Prompt(ctx, "Review this function...")
```

## Tools

```go
type WeatherTool struct{}

func (t *WeatherTool) Name() string        { return "get_weather" }
func (t *WeatherTool) Description() string { return "Get weather for a city" }
func (t *WeatherTool) Parameters() any {
    return struct {
        City string `json:"city" jsonschema:"required,description=City name"`
    }{}
}
func (t *WeatherTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
    var p struct{ City string }
    json.Unmarshal(args, &p)
    return map[string]any{"temp_f": 72, "condition": "sunny"}, nil
}
```

## Provider-Native Web Search

`aisdk.WebSearch` gives agents real-time web access by delegating to the AI provider's own search infrastructure — no third-party API key needed. Add it to the same `Tools` slice as your client-side tools.

```go
agent := aisdk.NewBaseAgent(aisdk.AgentConfig{
    Model:        "smart",
    Instructions: "You are a research assistant with access to the web.",
    Tools: []aisdk.Tool{
        &aisdk.WebSearch{},
    },
})

result, _ := agent.Prompt(ctx, "What are the latest Go releases?")
```

Switch providers in `config/ai.go` — the agent code stays the same:

| Provider | How it works |
|----------|-------------|
| **OpenAI** | Uses the [Responses API](https://platform.openai.com/docs/guides/tools-web-search) (`/v1/responses`) with `{"type": "web_search"}`. Model decides when to search. Uses the same model IDs as your tier aliases (`providers/openai`). |
| **Anthropic** | Uses `web_search_20250305` built-in tool in the Messages API. Any Claude model. |

### Options

```go
&aisdk.WebSearch{
    // Limit how many searches the provider may perform (Anthropic: max_uses).
    MaxResults: 5,

    // Restrict results to specific domains (both providers).
    AllowedDomains: []string{"pubmed.ncbi.nlm.nih.gov", "who.int", "cdc.gov"},

    // Refine results by user location (both providers).
    UserLocation: &aisdk.WebSearchLocation{
        Country:  "US",
        City:     "New York",
        Region:   "New York",
        Timezone: "America/New_York", // OpenAI only
    },
}
```

### Mixing with client-side tools

`Tools` is a single slice. The agent inspects each entry: anything implementing `Executable` is invoked locally via `Execute`; anything implementing `BuiltinTool` (like `WebSearch`) is forwarded to the provider as a native capability.

```go
Tools: []aisdk.Tool{
    &aisdk.WebSearch{}, // provider handles this server-side
    &WeatherTool{},     // your code handles this via Execute()
    &CalendarTool{},
},
```

## Conversations

```go
coach := agents.NewSalesCoach()
coach.ForUser("user-42")

coach.Prompt(ctx, "What's my pipeline?")
coach.Prompt(ctx, "How about Q2?") // automatically has context

// Resume later (e.g. next HTTP request)
coach.ContinueLast("user-42")
```

## Middleware

```go
func logging(ctx context.Context, p *aisdk.Prompt, next aisdk.NextFunc) (*aisdk.Response, error) {
    start := time.Now()
    resp, err := next(ctx, p)
    slog.Info("ai", "duration", time.Since(start), "tokens", resp.Usage.TotalTokens)
    return resp, err
}
```

## HTTP Handlers

```go
import "github.com/sadhakbj/aisdk-go/transport"

mux := http.NewServeMux()
mux.Handle("/api/chat/sse", transport.SSEHandler(agent))
mux.Handle("/api/chat", transport.VercelHandler(agent)) // Next.js useChat()
```

## Retry

By default, the SDK automatically retries on rate limit and provider overload errors using
exponential backoff with jitter (3 retries, 500ms initial delay, 30s cap).

```go
aisdk.Configure(&aisdk.Config{
    // ...providers...
    Retry: &aisdk.RetryConfig{
        MaxRetries:   5,
        InitialDelay: 1 * time.Second,
        MaxDelay:     60 * time.Second,
    },
})
```

Set `MaxRetries: 0` to disable retries entirely. When a `RateLimitedError` includes a
`RetryAfter` hint from the provider, that value is used instead of the backoff calculation.

## Testing

```go
import aisdktest "github.com/sadhakbj/aisdk-go/testing"

func TestCoach(t *testing.T) {
    fake := aisdktest.NewFakeApp(t)  // sets global default automatically
    fake.FakeText(aisdktest.Response("Revenue is up 15%."))

    coach := agents.NewSalesCoach()
    result, _ := coach.Prompt(ctx, "How are numbers?")

    assert.Equal(t, "Revenue is up 15%.", result.Text)
    fake.AssertPrompted(t, func(p *aisdk.Prompt) bool {
        return strings.Contains(p.Text, "numbers")
    })
}
```

## Providers

### OpenAI

```go
&openai.Config{
    APIKey:       os.Getenv("OPENAI_API_KEY"),
    BaseURL:      "",  // optional, for proxies
    Organization: "",  // optional
}
// Built-in: smart → gpt-5.4-pro, fast → gpt-5.4-nano, default → gpt-5.4
```

### Anthropic

```go
&anthropic.Config{
    APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
    BaseURL: "",  // optional
}
// Built-in: smart → claude-opus-4-7, fast → claude-haiku-4-5-20251001, default → claude-sonnet-4-6
```

### Adding a Provider

Implement the `Provider` and `TextModel` interfaces (including `SmartModel()` and `FastModel()`) and call `aisdk.RegisterProvider()` in your `init()` function.

## Custom Model Aliases (Optional)

If you need aliases beyond the built-in "smart"/"fast"/"default", add them to your config:

```go
aisdk.Configure(&aisdk.Config{
    // ...providers...
    Models: map[string]string{
        "reasoning": "openai/o1",
        "code":      "anthropic/claude-sonnet-4-6",
    },
})

result, _ := aisdk.GenerateText(ctx, aisdk.TextParams{Model: "reasoning", Prompt: "..."})
```

## License

MIT

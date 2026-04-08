# aisdk-go

[![Go Reference](https://pkg.go.dev/badge/github.com/sadhakbj/aisdk-go.svg)](https://pkg.go.dev/github.com/sadhakbj/aisdk-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/sadhakbj/aisdk-go)](https://goreportcard.com/report/github.com/sadhakbj/aisdk-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-%3E%3D%201.24-007d9c)](https://go.dev/doc/devel/release)

The unified AI SDK for Go — configure once, swap providers freely, test easily.

Stop wiring provider SDKs manually. `aisdk-go` gives Go applications a single, clean interface
to OpenAI, Anthropic, and more — the same philosophy that [Laravel AI SDK](https://laravel.com/docs/ai)
brought to PHP, built natively for Go.

```
go get github.com/sadhakbj/aisdk-go
```

## Features

- **Configure once, use everywhere** — Just credentials, like Laravel's `config/ai.php`
- **Providers know their models** — `"smart"`, `"fast"` resolved from the provider itself (like Laravel's `smartestTextModel()`)
- **Multi-provider** — OpenAI and Anthropic out of the box, extensible via `Provider` interface
- **Streaming** — Channel-based streaming with typed events
- **Structured output** — `GenerateObject[T]()` with generics for compile-time type safety
- **Tool calling** — Auto multi-step tool loop with JSON schema from struct tags
- **Built-in web search** — Drop-in `WebSearchTool` backed by Brave Search or SerpAPI
- **Conversations** — Pluggable `ConversationStore` with in-memory default
- **Middleware** — Intercept/modify prompts before they reach the model
- **HTTP handlers** — SSE and Vercel AI SDK Data Stream Protocol (works with Next.js `useChat()`)
- **Automatic retry** — Exponential backoff with jitter for rate limit and overload errors
- **Testing** — `FakeApp`, sequential fake responses, `AssertPrompted`, `PreventStrayPrompts`
- **Zero framework dependency** — Works with stdlib `net/http`, Chi, Gin, Echo, Fiber

## Getting Started

### 1. Configure

Create `config/ai.go` — your single source of truth (like Laravel's `config/ai.php`):

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
    // "smart" → resolved from the provider (OpenAI → gpt-4o)
    result, _ := aisdk.GenerateText(context.Background(), aisdk.TextParams{
        Model:  "smart",
        Prompt: "Explain goroutines in one paragraph.",
    })
    fmt.Println(result.Text)
}
```

## Built-in Model Aliases

Each provider defines its own model tiers — like Laravel's `defaultTextModel()`, `smartestTextModel()`, `cheapestTextModel()`:

| Alias       | OpenAI          | Anthropic                    |
|-------------|-----------------|------------------------------|
| `"smart"`   | `gpt-4o`        | `claude-sonnet-4-20250514`   |
| `"fast"`    | `gpt-4o-mini`   | `claude-haiku-3-5-20241022`  |
| `"default"` | `gpt-4o`        | `claude-sonnet-4-20250514`   |

```go
// Uses default provider's smart model
aisdk.GenerateText(ctx, aisdk.TextParams{Model: "smart", Prompt: "..."})

// Uses specific provider's fast model
aisdk.GenerateText(ctx, aisdk.TextParams{Model: "anthropic/fast", Prompt: "..."})

// Or use explicit model names
aisdk.GenerateText(ctx, aisdk.TextParams{Model: "openai/gpt-4o", Prompt: "..."})
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
result, _ := aisdk.Quick(aisdk.AgentConfig{
    Instructions: "You are a code reviewer.",
}).Prompt(ctx, "Review this function...")
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

## Built-in Web Search Tool

`aisdk-go` ships a ready-to-use `WebSearchTool` in the `tools` sub-package. Drop it into any agent to give it real-time web access — no custom tool implementation needed.

```go
import "github.com/sadhakbj/aisdk-go/tools"

agent := aisdk.NewBaseAgent(aisdk.AgentConfig{
    Model:        "smart",
    Instructions: "You are a research assistant. Use web_search to find up-to-date information.",
    Tools: []aisdk.Tool{
        tools.NewBraveWebSearch(os.Getenv("BRAVE_API_KEY")),
    },
    MaxSteps: 5,
})

result, _ := agent.Prompt(ctx, "What are the latest Go releases?")
```

### Supported Search Backends

| Backend | Constructor | API key source |
|---------|-------------|----------------|
| [Brave Search](https://brave.com/search/api/) | `tools.NewBraveWebSearch(apiKey)` | [brave.com/search/api](https://brave.com/search/api/) — free tier available |
| [SerpAPI](https://serpapi.com/) | `tools.NewSerpAPIWebSearch(apiKey)` | [serpapi.com](https://serpapi.com/) |

### Custom Search Backend

Implement the `tools.SearchProvider` interface to plug in any search API:

```go
type SearchProvider interface {
    Search(ctx context.Context, query string, maxResults int) ([]tools.WebSearchResult, error)
}

tool := tools.NewWebSearch(tools.WebSearchConfig{
    Provider:   &MySearchProvider{},
    MaxResults: 5,
    Name:        "my_search",         // optional, default: "web_search"
    Description: "Search my index.",  // optional
})
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
// Built-in: smart → gpt-4o, fast → gpt-4o-mini
```

### Anthropic

```go
&anthropic.Config{
    APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
    BaseURL: "",  // optional
}
// Built-in: smart → claude-sonnet-4-20250514, fast → claude-haiku-3-5-20241022
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
        "code":      "anthropic/claude-sonnet-4-20250514",
    },
})

result, _ := aisdk.GenerateText(ctx, aisdk.TextParams{Model: "reasoning", Prompt: "..."})
```

## License

MIT

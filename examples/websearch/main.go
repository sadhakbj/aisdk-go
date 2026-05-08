// Example: agent with provider-native web search.
//
// aisdk.WebSearch delegates searching to the AI provider's own servers —
// no third-party API key required.
//
// Provider behaviour:
//
//	Anthropic — any Claude model; model decides when to search (conditional).
//	OpenAI    — uses the Responses API (/v1/responses) with your model + web_search.
//
// Switch providers by changing Default in examples/config/ai.go.
//
// Prerequisites (set whichever provider you want to use):
//
//	ANTHROPIC_API_KEY=sk-ant-...
//	OPENAI_API_KEY=sk-...
//
// OpenAI uses the Responses API (/v1/responses) with the same tier aliases as chat
// (default / smart / fast map to the model IDs in providers/openai).
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/sadhakbj/aisdk-go"
	_ "github.com/sadhakbj/aisdk-go/examples/config"
)

type ResearchAssistant struct {
	aisdk.BaseAgent
}

func NewResearchAssistant() *ResearchAssistant {
	return &ResearchAssistant{
		BaseAgent: aisdk.NewBaseAgent(aisdk.AgentConfig{
			Model: "default",
			Instructions: "You are a helpful research assistant with access to the web. " +
				"Search for accurate, up-to-date information when needed.",
			Tools: []aisdk.Tool{
				&aisdk.WebSearch{MaxResults: 5},
			},
		}),
	}
}

func main() {
	ctx := context.Background()

	assistant := NewResearchAssistant()

	result, err := assistant.Prompt(ctx, "What is the current latest go version and when was it released?")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Provider: %s | Model: %s\n\n", result.Provider, result.Model)
	fmt.Println(result.Text)
}

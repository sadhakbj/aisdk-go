// Example: agent with built-in WebSearchTool.
//
// This example shows how to use the tools.NewBraveWebSearch() tool
// (or tools.NewSerpAPIWebSearch()) to give an agent the ability to
// search the web for up-to-date information.
//
// Prerequisites:
//
//	OPENAI_API_KEY=sk-...
//	BRAVE_API_KEY=...   (get a free key at https://brave.com/search/api/)
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/sadhakbj/aisdk-go"
	_ "github.com/sadhakbj/aisdk-go/examples/config"
	"github.com/sadhakbj/aisdk-go/tools"
)

type ResearchAssistant struct {
	aisdk.BaseAgent
}

func NewResearchAssistant() *ResearchAssistant {
	return &ResearchAssistant{
		BaseAgent: aisdk.NewBaseAgent(aisdk.AgentConfig{
			Model: "smart",
			Instructions: "You are a helpful research assistant with access to the web. " +
				"When asked about current events or facts you're unsure about, use the web_search tool " +
				"to look up accurate, up-to-date information before answering.",
			Tools: []aisdk.Tool{
				tools.NewBraveWebSearch(os.Getenv("BRAVE_API_KEY")),
			},
			MaxSteps: 5,
		}),
	}
}

func main() {
	ctx := context.Background()

	assistant := NewResearchAssistant()

	result, err := assistant.Prompt(ctx, "What are the latest Go programming language releases?")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:", result.Text)
	fmt.Printf("Steps: %d\n", len(result.Steps))
	fmt.Printf("Tool calls: %d\n", len(result.ToolCalls))
	for _, tc := range result.ToolCalls {
		fmt.Printf("  - %s(%s)\n", tc.Name, string(tc.Arguments))
	}
}

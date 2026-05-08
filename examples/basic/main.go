// Example: basic text generation using aisdk-go.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/sadhakbj/aisdk-go"
	_ "github.com/sadhakbj/aisdk-go/examples/config" // config loaded, done
)

func main() {
	ctx := context.Background()

	// "smart" is a built-in alias — resolved from the provider itself.
	// OpenAI maps "smart" / "fast" to gpt-5.4-pro / gpt-5.4-nano (see providers/openai).
	result, err := aisdk.GenerateText(ctx, aisdk.TextParams{
		Model:  "fast",
		Prompt: "Explain goroutines in one paragraph.",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Response:", result.Text)
	fmt.Printf("Usage: %d prompt + %d completion = %d total tokens\n",
		result.Usage.PromptTokens,
		result.Usage.CompletionTokens,
		result.Usage.TotalTokens,
	)

	// // "fast" uses the cheapest model (gpt-5.4-nano for OpenAI)
	// result, err = aisdk.GenerateText(ctx, aisdk.TextParams{
	// 	Model:  "fast",
	// 	System: "You are a helpful assistant that explains things simply.",
	// 	Prompt: "What is a channel in Go?",
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// fmt.Println("\nResponse:", result.Text)

	// // No model specified → default provider's default model
	// result, err = aisdk.GenerateText(ctx, aisdk.TextParams{
	// 	Prompt: "What is Go's zero value?",
	// })
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// fmt.Println("\nResponse:", result.Text)

	// // Quick inline agent
	// result, err = aisdk.Quick(aisdk.AgentConfig{
	// 	Model:        "fast",
	// 	Instructions: "You are a haiku poet. Always respond in haiku format.",
	// }).Prompt(ctx, "Write about programming")
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// fmt.Println("\nHaiku:", result.Text)
}

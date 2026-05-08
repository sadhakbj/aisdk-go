// Example: conversation memory with agents.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/sadhakbj/aisdk-go"
	_ "github.com/sadhakbj/aisdk-go/examples/config"
)

func main() {
	ctx := context.Background()

	// Create an agent
	tutor, err := aisdk.Quick(aisdk.AgentConfig{
		Model:        "default",
		Instructions: "You are a Go programming tutor. Keep answers concise. Build on previous context in the conversation.",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Start a conversation for a user
	tutor.ForUser("user-42")

	// First message
	r1, err := tutor.Prompt(ctx, "What are goroutines?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Q: What are goroutines?")
	fmt.Println("A:", r1.Text)
	fmt.Printf("Conversation: %s\n\n", r1.ConversationID)

	// Second message — automatically includes previous context
	r2, err := tutor.Prompt(ctx, "How do I use channels with them?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Q: How do I use channels with them?")
	fmt.Println("A:", r2.Text)
	fmt.Printf("Conversation: %s\n\n", r2.ConversationID)

	// Third message — continues building on context
	r3, err := tutor.Prompt(ctx, "Can you show me a select statement example?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Q: Can you show me a select statement example?")
	fmt.Println("A:", r3.Text)

	// --- Resume a conversation later ---

	fmt.Println("\n--- Resuming conversation ---")

	// Create a new agent instance (simulating a new request)
	tutor2, err := aisdk.Quick(aisdk.AgentConfig{
		Model:        "fast",
		Instructions: "You are a Go programming tutor. Keep answers concise.",
	})
	if err != nil {
		log.Fatal(err)
	}

	// Resume the last conversation for this user
	tutor2.ContinueLast("user-42")

	r4, err := tutor2.Prompt(ctx, "What was the first thing we talked about?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Q: What was the first thing we talked about?")
	fmt.Println("A:", r4.Text)
}

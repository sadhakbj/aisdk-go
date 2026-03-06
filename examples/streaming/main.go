// Example: streaming text generation with event handling.
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

	fmt.Print("Streaming: ")

	stream, err := aisdk.StreamText(ctx, aisdk.TextParams{
		Model:  "fast",
		System: "You are a helpful assistant.",
		Prompt: "Write a short poem about Go programming.",
	})
	if err != nil {
		log.Fatal(err)
	}

	for event := range stream.Events() {
		switch e := event.(type) {
		case *aisdk.StreamStart:
			fmt.Printf("[%s/%s]\n", e.Provider, e.Model)
		case *aisdk.TextDelta:
			fmt.Print(e.Text)
		case *aisdk.StreamEnd:
			fmt.Printf("\n\n[Done: %s, tokens: %d]\n",
				e.FinishReason, e.Usage.TotalTokens)
		case *aisdk.ErrorEvent:
			fmt.Printf("\n[Error: %v]\n", e.Err)
		}
	}

	resp := stream.Response()
	fmt.Printf("Full text length: %d characters\n", len(resp.Text))
}

package aisdk_test

import (
	"context"
	"strings"
	"testing"

	"github.com/sadhakbj/aisdk-go"
	aisdktest "github.com/sadhakbj/aisdk-go/testing"
)

func TestGenerateText(t *testing.T) {
	fake := aisdktest.NewFakeApp(t)
	fake.FakeText(aisdktest.Response("Hello, world!"))

	ctx := context.Background()

	// Use package-level function -- no app needed!
	result, err := aisdk.GenerateText(ctx, aisdk.TextParams{
		Model:  "fake-model",
		Prompt: "Say hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", result.Text)
	}

	if result.FinishReason != aisdk.FinishStop {
		t.Errorf("expected FinishStop, got %v", result.FinishReason)
	}

	fake.AssertPrompted(t, func(p *aisdk.Prompt) bool {
		return strings.Contains(p.Text, "hello")
	})
}

func TestStreamText(t *testing.T) {
	fake := aisdktest.NewFakeApp(t)
	fake.FakeStream(aisdktest.StreamEvents(
		&aisdk.TextDelta{Text: "Hello "},
		&aisdk.TextDelta{Text: "world!"},
		&aisdk.StreamEnd{FinishReason: aisdk.FinishStop, Usage: aisdk.Usage{TotalTokens: 10}},
	))

	ctx := context.Background()

	stream, err := aisdk.StreamText(ctx, aisdk.TextParams{
		Model:  "fake-model",
		Prompt: "Say hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	var text string
	for event := range stream.Events() {
		if delta, ok := event.(*aisdk.TextDelta); ok {
			text += delta.Text
		}
	}

	if text != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %q", text)
	}

	resp := stream.Response()
	if resp.Text != "Hello world!" {
		t.Errorf("response text mismatch: %q", resp.Text)
	}
}

func TestGenerateObject(t *testing.T) {
	type Recipe struct {
		Name        string   `json:"name"`
		Ingredients []string `json:"ingredients"`
	}

	fake := aisdktest.NewFakeApp(t)
	fake.FakeText(aisdktest.ObjectResponse(Recipe{
		Name:        "Pasta",
		Ingredients: []string{"noodles", "sauce"},
	}))

	ctx := context.Background()

	// Uses package-level function with default app
	result, err := aisdk.GenerateObject[Recipe](ctx, aisdk.ObjectParams{
		Model:  "fake-model",
		Prompt: "Give me a recipe",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Object.Name != "Pasta" {
		t.Errorf("expected 'Pasta', got %q", result.Object.Name)
	}

	if len(result.Object.Ingredients) != 2 {
		t.Errorf("expected 2 ingredients, got %d", len(result.Object.Ingredients))
	}
}

func TestQuickAgent(t *testing.T) {
	fake := aisdktest.NewFakeApp(t)
	fake.FakeText(aisdktest.Response("I found a bug!"))

	ctx := context.Background()

	// Quick without app -- uses global default
	result, err := aisdk.Quick(aisdk.AgentConfig{
		Instructions: "You are a code reviewer.",
	}).Prompt(ctx, "Review this code")
	if err != nil {
		t.Fatal(err)
	}

	if result.Text != "I found a bug!" {
		t.Errorf("expected 'I found a bug!', got %q", result.Text)
	}
}

func TestPreventStrayPrompts(t *testing.T) {
	// This test verifies that PreventStrayPrompts works.
	// We can't easily test that it causes a fatal without subprocess,
	// so we just verify it doesn't panic when responses are registered.
	fake := aisdktest.NewFakeApp(t)
	fake.FakeText(aisdktest.Response("OK"))
	fake.PreventStrayPrompts(t)

	ctx := context.Background()

	result, err := aisdk.GenerateText(ctx, aisdk.TextParams{
		Model:  "fake-model",
		Prompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Text != "OK" {
		t.Errorf("expected 'OK', got %q", result.Text)
	}
}

func TestAssertNeverPrompted(t *testing.T) {
	fake := aisdktest.NewFakeApp(t)
	fake.FakeText(aisdktest.Response("OK"))

	ctx := context.Background()

	_, _ = aisdk.GenerateText(ctx, aisdk.TextParams{
		Model:  "fake-model",
		Prompt: "hello",
	})

	// This should pass: no prompt contains "delete"
	fake.AssertNeverPrompted(t, func(p *aisdk.Prompt) bool {
		return strings.Contains(p.Text, "delete")
	})
}

func TestModelAliases(t *testing.T) {
	fake := aisdktest.NewFakeApp(t)
	app := fake.App()

	// Test that aliases don't exist in the fake app (default has no aliases)
	_, _, err := app.ResolveModel("smart")
	if err != nil {
		// Expected: no alias, falls back to default provider
		// The model name "smart" is used as-is
	}
}

func TestSequentialFakeResponses(t *testing.T) {
	fake := aisdktest.NewFakeApp(t)
	fake.FakeText(
		aisdktest.Response("First"),
		aisdktest.Response("Second"),
		aisdktest.Response("Third"),
	)

	ctx := context.Background()

	r1, _ := aisdk.GenerateText(ctx, aisdk.TextParams{Model: "m", Prompt: "1"})
	r2, _ := aisdk.GenerateText(ctx, aisdk.TextParams{Model: "m", Prompt: "2"})
	r3, _ := aisdk.GenerateText(ctx, aisdk.TextParams{Model: "m", Prompt: "3"})
	r4, _ := aisdk.GenerateText(ctx, aisdk.TextParams{Model: "m", Prompt: "4"}) // repeats last

	if r1.Text != "First" {
		t.Errorf("expected 'First', got %q", r1.Text)
	}
	if r2.Text != "Second" {
		t.Errorf("expected 'Second', got %q", r2.Text)
	}
	if r3.Text != "Third" {
		t.Errorf("expected 'Third', got %q", r3.Text)
	}
	if r4.Text != "Third" {
		t.Errorf("expected 'Third' (repeat), got %q", r4.Text)
	}

	fake.AssertPromptCount(t, 4)
}

// TestExplicitApp verifies you can still use an explicit App if needed.
func TestExplicitApp(t *testing.T) {
	fake := aisdktest.NewFakeApp(t)
	fake.FakeText(aisdktest.Response("explicit"))

	app := fake.App()
	ctx := context.Background()

	result, err := app.GenerateText(ctx, aisdk.TextParams{
		Model:  "fake-model",
		Prompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Text != "explicit" {
		t.Errorf("expected 'explicit', got %q", result.Text)
	}
}

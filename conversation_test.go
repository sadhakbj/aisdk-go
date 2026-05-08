package aisdk

import (
	"testing"
)

func TestInMemoryStore(t *testing.T) {
	ctx := t.Context()
	store := NewInMemoryStore()

	// Store a conversation
	conv := &Conversation{
		UserID: "user-1",
		Title:  "Test conversation",
	}
	if err := store.Store(ctx, conv); err != nil {
		t.Fatal(err)
	}
	if conv.ID == "" {
		t.Error("expected conversation ID to be set")
	}

	// Load it back
	loaded, err := store.Load(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "Test conversation" {
		t.Errorf("expected title 'Test conversation', got %q", loaded.Title)
	}

	// Append messages
	err = store.AppendMessages(ctx, conv.ID, []Message{
		User("Hello"),
		Assistant("Hi there!"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify messages
	loaded, err = store.Load(ctx, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(loaded.Messages))
	}

	// Latest for user
	latest, err := store.LatestForUser(ctx, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != conv.ID {
		t.Errorf("expected latest conversation to be %q, got %q", conv.ID, latest.ID)
	}

	// Not found errors
	_, err = store.Load(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent conversation")
	}

	_, err = store.LatestForUser(ctx, "nonexistent-user")
	if err == nil {
		t.Error("expected error for nonexistent user")
	}
}

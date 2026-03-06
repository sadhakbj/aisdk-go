package aisdk

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Conversation represents a stored conversation.
type Conversation struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Title     string    `json:"title"`
	Messages  []Message `json:"messages"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ConversationStore is the interface for persisting conversations.
// Implement this to plug in your own storage backend (Redis, SQL, etc.).
// The SDK ships with InMemoryStore as a sensible default.
type ConversationStore interface {
	// Store saves a new conversation.
	Store(ctx context.Context, conv *Conversation) error

	// Load retrieves a conversation by ID.
	Load(ctx context.Context, id string) (*Conversation, error)

	// LatestForUser returns the most recent conversation for a user.
	LatestForUser(ctx context.Context, userID string) (*Conversation, error)

	// AppendMessages adds messages to an existing conversation.
	AppendMessages(ctx context.Context, id string, msgs []Message) error
}

// --- InMemoryStore ---

// InMemoryStore is a simple in-memory ConversationStore.
// Suitable for development and testing.
type InMemoryStore struct {
	mu            sync.RWMutex
	conversations map[string]*Conversation
	userIndex     map[string][]string // userID -> []conversationID (ordered by time)
	counter       int
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		conversations: make(map[string]*Conversation),
		userIndex:     make(map[string][]string),
	}
}

func (s *InMemoryStore) Store(_ context.Context, conv *Conversation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if conv.ID == "" {
		s.counter++
		conv.ID = fmt.Sprintf("conv-%d", s.counter)
	}

	now := time.Now()
	if conv.CreatedAt.IsZero() {
		conv.CreatedAt = now
	}
	conv.UpdatedAt = now

	s.conversations[conv.ID] = conv
	s.userIndex[conv.UserID] = append(s.userIndex[conv.UserID], conv.ID)
	return nil
}

func (s *InMemoryStore) Load(_ context.Context, id string) (*Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	conv, ok := s.conversations[id]
	if !ok {
		return nil, fmt.Errorf("conversation %q not found", id)
	}
	return conv, nil
}

func (s *InMemoryStore) LatestForUser(_ context.Context, userID string) (*Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids, ok := s.userIndex[userID]
	if !ok || len(ids) == 0 {
		return nil, fmt.Errorf("no conversations found for user %q", userID)
	}

	latestID := ids[len(ids)-1]
	return s.conversations[latestID], nil
}

func (s *InMemoryStore) AppendMessages(_ context.Context, id string, msgs []Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	conv, ok := s.conversations[id]
	if !ok {
		return fmt.Errorf("conversation %q not found", id)
	}

	conv.Messages = append(conv.Messages, msgs...)
	conv.UpdatedAt = time.Now()
	return nil
}

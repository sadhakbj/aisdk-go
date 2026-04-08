package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sadhakbj/aisdk-go/tools"
)

// --- WebSearchTool unit tests ---

type mockSearchProvider struct {
	results []tools.WebSearchResult
	err     error
	gotQuery      string
	gotMaxResults int
}

func (m *mockSearchProvider) Search(_ context.Context, query string, maxResults int) ([]tools.WebSearchResult, error) {
	m.gotQuery = query
	m.gotMaxResults = maxResults
	return m.results, m.err
}

func TestWebSearchTool_Name(t *testing.T) {
	tool := tools.NewWebSearch(tools.WebSearchConfig{
		Provider: &mockSearchProvider{},
	})
	if tool.Name() != "web_search" {
		t.Errorf("expected name 'web_search', got %q", tool.Name())
	}
}

func TestWebSearchTool_CustomName(t *testing.T) {
	tool := tools.NewWebSearch(tools.WebSearchConfig{
		Provider: &mockSearchProvider{},
		Name:     "my_search",
	})
	if tool.Name() != "my_search" {
		t.Errorf("expected name 'my_search', got %q", tool.Name())
	}
}

func TestWebSearchTool_Description(t *testing.T) {
	tool := tools.NewWebSearch(tools.WebSearchConfig{
		Provider: &mockSearchProvider{},
	})
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestWebSearchTool_CustomDescription(t *testing.T) {
	desc := "custom description"
	tool := tools.NewWebSearch(tools.WebSearchConfig{
		Provider:    &mockSearchProvider{},
		Description: desc,
	})
	if tool.Description() != desc {
		t.Errorf("expected description %q, got %q", desc, tool.Description())
	}
}

func TestWebSearchTool_Parameters(t *testing.T) {
	tool := tools.NewWebSearch(tools.WebSearchConfig{
		Provider: &mockSearchProvider{},
	})
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
}

func TestWebSearchTool_Execute(t *testing.T) {
	mock := &mockSearchProvider{
		results: []tools.WebSearchResult{
			{Title: "Go Language", URL: "https://go.dev", Snippet: "Go is an open source programming language."},
			{Title: "Go Blog", URL: "https://go.dev/blog", Snippet: "The Go blog."},
		},
	}
	tool := tools.NewWebSearch(tools.WebSearchConfig{
		Provider:   mock,
		MaxResults: 3,
	})

	args, _ := json.Marshal(map[string]string{"query": "golang"})
	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	results, ok := result.([]tools.WebSearchResult)
	if !ok {
		t.Fatalf("expected []WebSearchResult, got %T", result)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if mock.gotQuery != "golang" {
		t.Errorf("expected query 'golang', got %q", mock.gotQuery)
	}
	if mock.gotMaxResults != 3 {
		t.Errorf("expected maxResults 3, got %d", mock.gotMaxResults)
	}
}

func TestWebSearchTool_Execute_EmptyQuery(t *testing.T) {
	tool := tools.NewWebSearch(tools.WebSearchConfig{
		Provider: &mockSearchProvider{},
	})
	args, _ := json.Marshal(map[string]string{"query": ""})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Error("expected error for empty query")
	}
}

func TestWebSearchTool_Execute_InvalidArgs(t *testing.T) {
	tool := tools.NewWebSearch(tools.WebSearchConfig{
		Provider: &mockSearchProvider{},
	})
	_, err := tool.Execute(context.Background(), json.RawMessage("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON args")
	}
}

func TestWebSearchTool_Execute_ProviderError(t *testing.T) {
	mock := &mockSearchProvider{err: errors.New("network error")}
	tool := tools.NewWebSearch(tools.WebSearchConfig{
		Provider: mock,
	})
	args, _ := json.Marshal(map[string]string{"query": "test"})
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Error("expected error when provider fails")
	}
}

func TestWebSearchTool_DefaultMaxResults(t *testing.T) {
	mock := &mockSearchProvider{}
	tool := tools.NewWebSearch(tools.WebSearchConfig{
		Provider: mock,
	})
	args, _ := json.Marshal(map[string]string{"query": "test"})
	_, _ = tool.Execute(context.Background(), args)
	if mock.gotMaxResults != 5 {
		t.Errorf("expected default maxResults 5, got %d", mock.gotMaxResults)
	}
}

// --- BraveSearchProvider integration tests (using httptest server) ---

func TestBraveSearchProvider_Search(t *testing.T) {
	// Start a local HTTP server that mimics the Brave Search API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("q") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"web": map[string]any{
				"results": []map[string]any{
					{"title": "Go", "url": "https://go.dev", "description": "Go programming language"},
					{"title": "Go Blog", "url": "https://go.dev/blog", "description": "The official Go blog"},
				},
			},
		})
	}))
	defer server.Close()

	provider := newBraveProviderWithBaseURL("test-key", server.URL)
	results, err := provider.Search(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Go" {
		t.Errorf("expected title 'Go', got %q", results[0].Title)
	}
	if results[0].URL != "https://go.dev" {
		t.Errorf("expected URL 'https://go.dev', got %q", results[0].URL)
	}
	if results[0].Snippet != "Go programming language" {
		t.Errorf("expected snippet 'Go programming language', got %q", results[0].Snippet)
	}
}

func TestBraveSearchProvider_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	provider := newBraveProviderWithBaseURL("test-key", server.URL)
	_, err := provider.Search(context.Background(), "golang", 5)
	if err == nil {
		t.Error("expected error for non-200 HTTP status")
	}
}

// --- SerpAPIProvider integration tests (using httptest server) ---

func TestSerpAPIProvider_Search(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"organic_results": []map[string]any{
				{"title": "Result 1", "link": "https://example.com/1", "snippet": "First result"},
				{"title": "Result 2", "link": "https://example.com/2", "snippet": "Second result"},
			},
		})
	}))
	defer server.Close()

	provider := newSerpAPIProviderWithBaseURL("test-key", server.URL)
	results, err := provider.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if results[0].Title != "Result 1" {
		t.Errorf("expected title 'Result 1', got %q", results[0].Title)
	}
	if results[0].URL != "https://example.com/1" {
		t.Errorf("expected URL 'https://example.com/1', got %q", results[0].URL)
	}
	if results[0].Snippet != "First result" {
		t.Errorf("expected snippet 'First result', got %q", results[0].Snippet)
	}
}

func TestSerpAPIProvider_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := newSerpAPIProviderWithBaseURL("test-key", server.URL)
	_, err := provider.Search(context.Background(), "test", 5)
	if err == nil {
		t.Error("expected error for non-200 HTTP status")
	}
}

// --- Helpers to inject test server URLs ---

func newBraveProviderWithBaseURL(apiKey, baseURL string) *tools.BraveSearchProvider {
	p := tools.NewBraveSearchProvider(apiKey)
	p.SetBaseURL(baseURL)
	return p
}

func newSerpAPIProviderWithBaseURL(apiKey, baseURL string) *tools.SerpAPIProvider {
	p := tools.NewSerpAPIProvider(apiKey)
	p.SetBaseURL(baseURL)
	return p
}

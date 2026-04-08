// Package tools provides built-in tools for use with aisdk-go agents.
//
// The WebSearchTool allows agents to search the web for up-to-date information.
// It supports multiple search backends via the SearchProvider interface:
//
//   - BraveSearchProvider  — Brave Search API (https://brave.com/search/api/)
//   - SerpAPIProvider      — SerpAPI (https://serpapi.com/)
//
// Quick start with Brave Search:
//
//	agent := aisdk.NewBaseAgent(aisdk.AgentConfig{
//	    Tools: []aisdk.Tool{
//	        tools.NewBraveWebSearch(os.Getenv("BRAVE_API_KEY")),
//	    },
//	})
package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// WebSearchResult represents a single search result.
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// SearchProvider is the interface implemented by search backends.
// Implement this interface to plug in any search API.
type SearchProvider interface {
	Search(ctx context.Context, query string, maxResults int) ([]WebSearchResult, error)
}

// WebSearchConfig configures a WebSearchTool.
type WebSearchConfig struct {
	// Provider is the search backend to use. Required.
	Provider SearchProvider

	// MaxResults is the maximum number of results to return (default: 5).
	MaxResults int

	// Name overrides the tool name (default: "web_search").
	Name string

	// Description overrides the tool description.
	Description string
}

// WebSearchTool is a built-in tool that performs web searches.
// It implements the aisdk.Tool interface and can be added to any agent.
type WebSearchTool struct {
	name        string
	description string
	provider    SearchProvider
	maxResults  int
}

// NewWebSearch creates a new WebSearchTool with the given configuration.
func NewWebSearch(config WebSearchConfig) *WebSearchTool {
	if config.MaxResults <= 0 {
		config.MaxResults = 5
	}
	name := config.Name
	if name == "" {
		name = "web_search"
	}
	description := config.Description
	if description == "" {
		description = "Search the web for current information. Use this to find up-to-date facts, news, or any information not in your training data."
	}
	return &WebSearchTool{
		name:        name,
		description: description,
		provider:    config.Provider,
		maxResults:  config.MaxResults,
	}
}

// Name returns the tool's unique name.
func (t *WebSearchTool) Name() string { return t.name }

// Description returns a human-readable description of what the tool does.
func (t *WebSearchTool) Description() string { return t.description }

// Parameters returns the tool's parameter schema.
func (t *WebSearchTool) Parameters() any {
	return struct {
		Query string `json:"query" jsonschema:"required,description=The web search query"`
	}{}
}

// Execute runs the web search with the provided JSON arguments.
func (t *WebSearchTool) Execute(ctx context.Context, args json.RawMessage) (any, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("web_search: invalid arguments: %w", err)
	}
	if params.Query == "" {
		return nil, fmt.Errorf("web_search: query is required")
	}

	results, err := t.provider.Search(ctx, params.Query, t.maxResults)
	if err != nil {
		return nil, fmt.Errorf("web_search: %w", err)
	}
	return results, nil
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// BraveSearchProvider implements SearchProvider using the Brave Search API.
// Get your free API key at https://brave.com/search/api/
type BraveSearchProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewBraveSearchProvider creates a new Brave Search provider with the given API key.
func NewBraveSearchProvider(apiKey string) *BraveSearchProvider {
	return &BraveSearchProvider{
		apiKey:     apiKey,
		baseURL:    "https://api.search.brave.com",
		httpClient: &http.Client{},
	}
}

// SetBaseURL overrides the Brave Search API base URL (useful for testing).
func (p *BraveSearchProvider) SetBaseURL(baseURL string) {
	p.baseURL = baseURL
}

// NewBraveWebSearch is a convenience constructor that creates a WebSearchTool
// backed by the Brave Search API.
//
//	tool := tools.NewBraveWebSearch(os.Getenv("BRAVE_API_KEY"))
func NewBraveWebSearch(apiKey string) *WebSearchTool {
	return NewWebSearch(WebSearchConfig{
		Provider: NewBraveSearchProvider(apiKey),
	})
}

// Search performs a web search using the Brave Search API.
func (p *BraveSearchProvider) Search(ctx context.Context, query string, maxResults int) ([]WebSearchResult, error) {
	endpoint := fmt.Sprintf(
		"%s/res/v1/web/search?q=%s&count=%d",
		p.baseURL,
		url.QueryEscape(query),
		maxResults,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("brave_search: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave_search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave_search: unexpected status %d", resp.StatusCode)
	}

	var apiResp braveAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("brave_search: decode response: %w", err)
	}

	results := make([]WebSearchResult, 0, len(apiResp.Web.Results))
	for _, item := range apiResp.Web.Results {
		results = append(results, WebSearchResult{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Description,
		})
	}
	return results, nil
}

// braveAPIResponse mirrors the relevant parts of the Brave Search API response.
type braveAPIResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

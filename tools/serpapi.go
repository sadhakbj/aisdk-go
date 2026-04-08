package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// SerpAPIProvider implements SearchProvider using SerpAPI.
// Get your API key at https://serpapi.com/
type SerpAPIProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewSerpAPIProvider creates a new SerpAPI provider with the given API key.
func NewSerpAPIProvider(apiKey string) *SerpAPIProvider {
	return &SerpAPIProvider{
		apiKey:     apiKey,
		baseURL:    "https://serpapi.com",
		httpClient: &http.Client{},
	}
}

// SetBaseURL overrides the SerpAPI base URL (useful for testing).
func (p *SerpAPIProvider) SetBaseURL(baseURL string) {
	p.baseURL = baseURL
}

// NewSerpAPIWebSearch is a convenience constructor that creates a WebSearchTool
// backed by SerpAPI.
//
//	tool := tools.NewSerpAPIWebSearch(os.Getenv("SERPAPI_API_KEY"))
func NewSerpAPIWebSearch(apiKey string) *WebSearchTool {
	return NewWebSearch(WebSearchConfig{
		Provider: NewSerpAPIProvider(apiKey),
	})
}

// Search performs a web search using the SerpAPI.
func (p *SerpAPIProvider) Search(ctx context.Context, query string, maxResults int) ([]WebSearchResult, error) {
	endpoint := fmt.Sprintf(
		"%s/search.json?q=%s&num=%d&api_key=%s",
		p.baseURL,
		url.QueryEscape(query),
		maxResults,
		url.QueryEscape(p.apiKey),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("serpapi: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("serpapi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("serpapi: unexpected status %d", resp.StatusCode)
	}

	var apiResp serpAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("serpapi: decode response: %w", err)
	}

	results := make([]WebSearchResult, 0, len(apiResp.OrganicResults))
	for _, item := range apiResp.OrganicResults {
		results = append(results, WebSearchResult{
			Title:   item.Title,
			URL:     item.Link,
			Snippet: item.Snippet,
		})
	}
	return results, nil
}

// serpAPIResponse mirrors the relevant parts of the SerpAPI response.
type serpAPIResponse struct {
	OrganicResults []struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
	} `json:"organic_results"`
}

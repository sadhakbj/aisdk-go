package anthropic

import aisdk "github.com/sadhakbj/aisdk-go"

const (
	defaultBaseURL = "https://api.anthropic.com/v1"
	defaultModel   = "claude-sonnet-4-6"
	apiVersion     = "2023-06-01"
)

// Config holds Anthropic-specific configuration.
type Config struct {
	// APIKey is the Anthropic API key.
	APIKey string

	// BaseURL overrides the API base URL.
	BaseURL string

	// DefaultModel is the default model to use. Defaults to "claude-sonnet-4-6".
	DefaultModel string
}

func (c *Config) IsProviderConfig() {}

// Ensure Config implements ProviderConfig at compile time.
var _ aisdk.ProviderConfig = (*Config)(nil)

func (c *Config) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

func (c *Config) defaultModel() string {
	if c.DefaultModel != "" {
		return c.DefaultModel
	}
	return defaultModel
}

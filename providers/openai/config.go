package openai

import aisdk "github.com/sadhakbj/aisdk-go"

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-5.4"
)

// Config holds OpenAI-specific configuration.
type Config struct {
	// APIKey is the OpenAI API key.
	APIKey string

	// BaseURL overrides the API base URL (useful for proxies or compatible APIs).
	BaseURL string

	// DefaultModel is the default model to use. Defaults to "gpt-5.4".
	DefaultModel string

	// Organization is the optional OpenAI organization ID.
	Organization string
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

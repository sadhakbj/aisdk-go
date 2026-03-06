package gemini

import aisdk "github.com/sadhakbj/aisdk-go"

const (
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	defaultModel   = "gemini-2.0-flash"
)

// Config holds Gemini-specific configuration.
type Config struct {
	// APIKey is the Google Gemini API key.
	APIKey string

	// BaseURL overrides the API base URL.
	BaseURL string

	// DefaultModel is the default model to use. Defaults to "gemini-2.0-flash".
	DefaultModel string
}

func (c *Config) IsProviderConfig() {}

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

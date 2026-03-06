package aisdk

import "time"

// PromptOption configures a single Prompt/Stream call.
type PromptOption func(*promptOptions)

type promptOptions struct {
	provider    string
	model       string
	temperature *float64
	maxTokens   *int
	timeout     *time.Duration
}

func defaultPromptOptions() *promptOptions {
	return &promptOptions{}
}

func applyPromptOptions(opts []PromptOption) *promptOptions {
	o := defaultPromptOptions()
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithProvider overrides the provider for a single call.
func WithProvider(provider string) PromptOption {
	return func(o *promptOptions) {
		o.provider = provider
	}
}

// WithModel overrides the model for a single call.
// Can be a full "provider/model" string or just "model" (uses current provider).
func WithModel(model string) PromptOption {
	return func(o *promptOptions) {
		o.model = model
	}
}

// WithTemperature overrides the temperature for a single call.
func WithTemperature(t float64) PromptOption {
	return func(o *promptOptions) {
		o.temperature = &t
	}
}

// WithMaxTokens overrides the max tokens for a single call.
func WithMaxTokens(n int) PromptOption {
	return func(o *promptOptions) {
		o.maxTokens = &n
	}
}

// WithTimeout overrides the timeout for a single call.
func WithTimeout(d time.Duration) PromptOption {
	return func(o *promptOptions) {
		o.timeout = &d
	}
}

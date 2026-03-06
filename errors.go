package aisdk

import (
	"errors"
	"fmt"
	"time"
)

// Failoverable is implemented by errors that indicate the request can be
// retried with a different provider or model.
type Failoverable interface {
	error
	IsFailoverable() bool
}

// IsFailoverable checks whether an error (or any in its chain) is failoverable.
func IsFailoverable(err error) bool {
	var f Failoverable
	if errors.As(err, &f) {
		return f.IsFailoverable()
	}
	return false
}

// AIError is the base error type for all SDK errors.
type AIError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Cause    error  `json:"-"`
}

func (e *AIError) Error() string {
	if e.Provider != "" {
		return fmt.Sprintf("aisdk [%s/%s]: %s", e.Provider, e.Code, e.Message)
	}
	return fmt.Sprintf("aisdk [%s]: %s", e.Code, e.Message)
}

func (e *AIError) Unwrap() error {
	return e.Cause
}

// RateLimitedError indicates the provider returned a rate limit response.
type RateLimitedError struct {
	AIError
	RetryAfter time.Duration `json:"retry_after,omitempty"`
}

func (e *RateLimitedError) IsFailoverable() bool { return true }

// NewRateLimitedError creates a new RateLimitedError.
func NewRateLimitedError(provider string, retryAfter time.Duration, cause error) *RateLimitedError {
	return &RateLimitedError{
		AIError: AIError{
			Code:     "rate_limited",
			Message:  "rate limited by provider",
			Provider: provider,
			Cause:    cause,
		},
		RetryAfter: retryAfter,
	}
}

// ProviderOverloadedError indicates the provider is temporarily overloaded.
type ProviderOverloadedError struct {
	AIError
}

func (e *ProviderOverloadedError) IsFailoverable() bool { return true }

// NewProviderOverloadedError creates a new ProviderOverloadedError.
func NewProviderOverloadedError(provider string, cause error) *ProviderOverloadedError {
	return &ProviderOverloadedError{
		AIError: AIError{
			Code:     "overloaded",
			Message:  "provider is overloaded",
			Provider: provider,
			Cause:    cause,
		},
	}
}

// ToolError indicates an error during tool execution.
type ToolError struct {
	AIError
	ToolName string `json:"tool_name"`
}

// NewToolError creates a new ToolError.
func NewToolError(toolName string, cause error) *ToolError {
	return &ToolError{
		AIError: AIError{
			Code:    "tool_error",
			Message: fmt.Sprintf("tool %q failed: %v", toolName, cause),
			Cause:   cause,
		},
		ToolName: toolName,
	}
}

// ProviderError indicates a generic provider-side error.
type ProviderError struct {
	AIError
	StatusCode int `json:"status_code,omitempty"`
}

// NewProviderError creates a new ProviderError.
func NewProviderError(provider string, statusCode int, message string, cause error) *ProviderError {
	return &ProviderError{
		AIError: AIError{
			Code:     "provider_error",
			Message:  message,
			Provider: provider,
			Cause:    cause,
		},
		StatusCode: statusCode,
	}
}

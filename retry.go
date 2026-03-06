package aisdk

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// RetryConfig configures automatic retry behavior for rate limit and overload errors.
// If not set in Config, sensible defaults are used (3 retries, 500ms initial delay).
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts after the initial call.
	// Set to 0 to disable retries entirely.
	MaxRetries int

	// InitialDelay is the base delay before the first retry.
	InitialDelay time.Duration

	// MaxDelay caps the exponential backoff growth.
	MaxDelay time.Duration
}

func defaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:   3,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     30 * time.Second,
	}
}

// effectiveRetry returns the retry config to use, falling back to defaults when
// the Config.Retry field is nil.
func (a *App) effectiveRetry() RetryConfig {
	if a.config.Retry != nil {
		return *a.config.Retry
	}
	return defaultRetryConfig()
}

// isRetryable reports whether the error should trigger a retry attempt.
// Only rate limit and provider overload errors are retryable.
func isRetryable(err error) bool {
	var rl *RateLimitedError
	var ol *ProviderOverloadedError
	return errors.As(err, &rl) || errors.As(err, &ol)
}

// retryDelay returns how long to wait before the next attempt.
// If the error is a RateLimitedError with a RetryAfter hint, that value is used.
// Otherwise exponential backoff with up to 20% random jitter is applied.
func retryDelay(err error, attempt int, cfg RetryConfig) time.Duration {
	var rl *RateLimitedError
	if errors.As(err, &rl) && rl.RetryAfter > 0 {
		return rl.RetryAfter
	}

	// Exponential: InitialDelay * 2^attempt
	delay := cfg.InitialDelay * (1 << uint(attempt))
	// Add up to 20% jitter to spread out thundering-herd retries
	jitter := time.Duration(rand.Int63n(int64(delay) / 5))
	delay += jitter

	if delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}
	return delay
}

// withRetry calls fn, retrying on retryable errors (rate limit, overload) up to
// cfg.MaxRetries additional attempts. Context cancellation stops retrying immediately.
func withRetry[T any](ctx context.Context, cfg RetryConfig, fn func() (T, error)) (T, error) {
	result, err := fn()
	for attempt := 0; err != nil && isRetryable(err) && attempt < cfg.MaxRetries; attempt++ {
		delay := retryDelay(err, attempt, cfg)
		select {
		case <-ctx.Done():
			var zero T
			return zero, ctx.Err()
		case <-time.After(delay):
		}
		result, err = fn()
	}
	return result, err
}

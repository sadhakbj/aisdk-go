package aisdk

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// fastRetry is a RetryConfig with sub-millisecond delays for quick tests.
func fastRetry(maxRetries int) RetryConfig {
	return RetryConfig{
		MaxRetries:   maxRetries,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     5 * time.Millisecond,
	}
}

func TestWithRetrySucceedsFirstTry(t *testing.T) {
	calls := 0
	got, err := withRetry(t.Context(), fastRetry(3), func() (string, error) {
		calls++
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("want %q, got %q", "ok", got)
	}
	if calls != 1 {
		t.Errorf("want 1 call, got %d", calls)
	}
}

func TestWithRetryRetriesOnRateLimit(t *testing.T) {
	calls := 0
	got, err := withRetry(t.Context(), fastRetry(3), func() (string, error) {
		calls++
		if calls < 3 {
			return "", NewRateLimitedError("test", 0, nil)
		}
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Errorf("want %q, got %q", "ok", got)
	}
	if calls != 3 {
		t.Errorf("want 3 calls (1 initial + 2 retries), got %d", calls)
	}
}

func TestWithRetryRetriesOnOverload(t *testing.T) {
	calls := 0
	_, err := withRetry(t.Context(), fastRetry(2), func() (string, error) {
		calls++
		return "", NewProviderOverloadedError("test", nil)
	})

	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !errors.As(err, new(*ProviderOverloadedError)) {
		t.Errorf("expected ProviderOverloadedError, got %T: %v", err, err)
	}
	if calls != 3 {
		t.Errorf("want 3 calls (1 initial + 2 retries), got %d", calls)
	}
}

func TestWithRetryGivesUpAfterMaxRetries(t *testing.T) {
	calls := 0
	_, err := withRetry(t.Context(), fastRetry(2), func() (string, error) {
		calls++
		return "", NewRateLimitedError("test", 0, nil)
	})

	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 3 {
		t.Errorf("want 3 calls (1 initial + 2 retries), got %d", calls)
	}
}

func TestWithRetryNonRetryableErrorReturnsImmediately(t *testing.T) {
	calls := 0
	_, err := withRetry(t.Context(), fastRetry(5), func() (string, error) {
		calls++
		return "", errors.New("plain error")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("non-retryable errors should not retry; got %d calls", calls)
	}
}

func TestWithRetryProviderErrorNotRetryable(t *testing.T) {
	// ProviderError (e.g. 400 bad request) is not retryable.
	calls := 0
	_, err := withRetry(t.Context(), fastRetry(5), func() (string, error) {
		calls++
		return "", NewProviderError("test", 400, "bad request", nil)
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("ProviderError should not retry; got %d calls", calls)
	}
}

func TestWithRetryRespectsContextCancellation(t *testing.T) {
	// Slow retry config so we can cancel mid-backoff.
	cfg := RetryConfig{
		MaxRetries:   5,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     1 * time.Second,
	}

	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	go func() {
		// Cancel after the first failure triggers backoff.
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := withRetry(ctx, cfg, func() (string, error) {
		calls++
		return "", NewRateLimitedError("test", 0, nil)
	})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	// Should bail well before InitialDelay completes.
	if elapsed >= 200*time.Millisecond {
		t.Errorf("withRetry didn't honor cancellation; ran for %s", elapsed)
	}
	if calls != 1 {
		t.Errorf("want 1 call before cancellation, got %d", calls)
	}
}

func TestWithRetryUsesRetryAfterFromError(t *testing.T) {
	// RateLimitedError with RetryAfter wins over computed backoff.
	cfg := RetryConfig{
		MaxRetries:   1,
		InitialDelay: 1 * time.Second, // would be slower if used
		MaxDelay:     1 * time.Second,
	}

	calls := 0
	start := time.Now()
	_, err := withRetry(t.Context(), cfg, func() (string, error) {
		calls++
		if calls == 1 {
			return "", NewRateLimitedError("test", 5*time.Millisecond, nil)
		}
		return "ok", nil
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("want 2 calls, got %d", calls)
	}
	if elapsed >= 500*time.Millisecond {
		t.Errorf("RetryAfter ignored; backoff took %s", elapsed)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"RateLimitedError", NewRateLimitedError("p", 0, nil), true},
		{"ProviderOverloadedError", NewProviderOverloadedError("p", nil), true},
		{"ProviderError", NewProviderError("p", 500, "internal", nil), false},
		{"plain error", errors.New("nope"), false},
		{"nil", nil, false},
		{"wrapped RateLimited", fmt.Errorf("outer: %w", NewRateLimitedError("p", 0, nil)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryable(tt.err); got != tt.want {
				t.Errorf("isRetryable(%v) = %v; want %v", tt.err, got, tt.want)
			}
		})
	}
}

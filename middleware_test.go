package aisdk

import (
	"context"
	"testing"
)

func TestMiddlewarePipeline(t *testing.T) {
	var order []string

	mw1 := func(ctx context.Context, p *Prompt, next NextFunc) (*Response, error) {
		order = append(order, "mw1-before")
		resp, err := next(ctx, p)
		order = append(order, "mw1-after")
		return resp, err
	}

	mw2 := func(ctx context.Context, p *Prompt, next NextFunc) (*Response, error) {
		order = append(order, "mw2-before")
		resp, err := next(ctx, p)
		order = append(order, "mw2-after")
		return resp, err
	}

	handler := func(ctx context.Context, p *Prompt) (*Response, error) {
		order = append(order, "handler")
		return &Response{Text: "OK"}, nil
	}

	ctx := t.Context()
	resp, err := runMiddleware(ctx, &Prompt{Text: "test"}, []Middleware{mw1, mw2}, handler)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Text != "OK" {
		t.Errorf("expected 'OK', got %q", resp.Text)
	}

	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("order[%d]: expected %q, got %q", i, v, order[i])
		}
	}
}

func TestMiddlewarePipelineEmpty(t *testing.T) {
	handler := func(ctx context.Context, p *Prompt) (*Response, error) {
		return &Response{Text: "direct"}, nil
	}

	ctx := t.Context()
	resp, err := runMiddleware(ctx, &Prompt{Text: "test"}, nil, handler)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Text != "direct" {
		t.Errorf("expected 'direct', got %q", resp.Text)
	}
}

func TestMiddlewareModifiesPrompt(t *testing.T) {
	mw := func(ctx context.Context, p *Prompt, next NextFunc) (*Response, error) {
		p.Prepend(System("Injected context"))
		return next(ctx, p)
	}

	handler := func(ctx context.Context, p *Prompt) (*Response, error) {
		if len(p.Messages) == 0 {
			t.Error("expected middleware to add a message")
		}
		if p.Messages[0].Role != RoleSystem {
			t.Errorf("expected system message, got %v", p.Messages[0].Role)
		}
		return &Response{Text: "OK"}, nil
	}

	ctx := t.Context()
	_, err := runMiddleware(ctx, &Prompt{Text: "test"}, []Middleware{mw}, handler)
	if err != nil {
		t.Fatal(err)
	}
}

package aisdk

import "context"

// Prompt wraps the data flowing through the middleware pipeline.
type Prompt struct {
	Text        string
	Messages    []Message
	Attachments []Attachment
	Agent       Agent // the agent being prompted (may be nil for direct calls)
}

// Prepend adds a message to the beginning of the message list.
func (p *Prompt) Prepend(msg Message) {
	p.Messages = append([]Message{msg}, p.Messages...)
}

// Append adds a message to the end of the message list.
func (p *Prompt) Append(msg Message) {
	p.Messages = append(p.Messages, msg)
}

// NextFunc is the function signature for the next handler in the middleware chain.
type NextFunc func(ctx context.Context, p *Prompt) (*Response, error)

// Middleware intercepts a prompt, can modify it, and calls next to continue.
type Middleware func(ctx context.Context, p *Prompt, next NextFunc) (*Response, error)

// runMiddleware executes a middleware pipeline around a core handler.
func runMiddleware(ctx context.Context, p *Prompt, middlewares []Middleware, handler NextFunc) (*Response, error) {
	if len(middlewares) == 0 {
		return handler(ctx, p)
	}

	// Build the chain from the inside out
	next := handler
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		currentNext := next
		next = func(ctx context.Context, p *Prompt) (*Response, error) {
			return mw(ctx, p, currentNext)
		}
	}

	return next(ctx, p)
}

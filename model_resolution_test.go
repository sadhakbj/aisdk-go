package aisdk

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// stubProvider is a minimal Provider used for testing model resolution. Each
// instance reports its tier model names via the StubName fields.
type stubProvider struct {
	id           string
	defaultModel string
	smartModel   string
	fastModel    string
}

func (p *stubProvider) ID() string                       { return p.id }
func (p *stubProvider) DefaultModel() string             { return p.defaultModel }
func (p *stubProvider) SmartModel() string               { return p.smartModel }
func (p *stubProvider) FastModel() string                { return p.fastModel }
func (p *stubProvider) TextModel(string) TextModel       { return nil }

type stubProviderConfig struct{ id string }

func (c *stubProviderConfig) IsProviderConfig() {}

// registerStubProvider registers a stub provider factory under name and
// returns a cleanup function. Concurrent calls are guarded by stubMu so
// parallel tests don't race on the global registry.
var stubMu sync.Mutex

func registerStubProvider(t *testing.T, name string, p *stubProvider) {
	t.Helper()
	stubMu.Lock()
	defer stubMu.Unlock()
	RegisterProvider(name, func(cfg ProviderConfig) (Provider, error) {
		return p, nil
	})
}

// newResolverApp builds an App wired up with the given stub providers. The
// first name listed becomes the default unless overrideDefault is non-empty.
func newResolverApp(t *testing.T, defaultName string, providers map[string]*stubProvider, customAliases map[string]string) *App {
	t.Helper()

	cfgProviders := map[string]ProviderConfig{}
	for name, p := range providers {
		registerStubProvider(t, name, p)
		cfgProviders[name] = &stubProviderConfig{id: name}
	}

	return New(&Config{
		Providers: cfgProviders,
		Default:   defaultName,
		Models:    customAliases,
	})
}

func TestResolveModelDefaultProviderDefaultModel(t *testing.T) {
	app := newResolverApp(t, "openai", map[string]*stubProvider{
		"openai": {id: "openai", defaultModel: "gpt-d", smartModel: "gpt-s", fastModel: "gpt-f"},
	}, nil)

	provider, model, err := app.ResolveModel("")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || model != "gpt-d" {
		t.Errorf("got (%s, %s); want (openai, gpt-d)", provider, model)
	}
}

func TestResolveModelBuiltinAliases(t *testing.T) {
	app := newResolverApp(t, "openai", map[string]*stubProvider{
		"openai": {id: "openai", defaultModel: "gpt-d", smartModel: "gpt-s", fastModel: "gpt-f"},
	}, nil)

	tests := []struct {
		alias string
		want  string
	}{
		{"smart", "gpt-s"},
		{"fast", "gpt-f"},
		{"default", "gpt-d"},
	}
	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			provider, model, err := app.ResolveModel(tt.alias)
			if err != nil {
				t.Fatal(err)
			}
			if provider != "openai" {
				t.Errorf("provider = %q; want openai", provider)
			}
			if model != tt.want {
				t.Errorf("model = %q; want %q", model, tt.want)
			}
		})
	}
}

func TestResolveModelExplicitProviderTier(t *testing.T) {
	app := newResolverApp(t, "openai", map[string]*stubProvider{
		"openai":    {id: "openai", defaultModel: "gpt-d", smartModel: "gpt-s", fastModel: "gpt-f"},
		"anthropic": {id: "anthropic", defaultModel: "claude-d", smartModel: "claude-s", fastModel: "claude-f"},
	}, nil)

	// Default provider is openai, but "anthropic/smart" should pick anthropic.
	provider, model, err := app.ResolveModel("anthropic/smart")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "anthropic" || model != "claude-s" {
		t.Errorf("got (%s, %s); want (anthropic, claude-s)", provider, model)
	}
}

func TestResolveModelExplicitProviderLiteralModel(t *testing.T) {
	app := newResolverApp(t, "openai", map[string]*stubProvider{
		"openai": {id: "openai", defaultModel: "gpt-d", smartModel: "gpt-s", fastModel: "gpt-f"},
	}, nil)

	provider, model, err := app.ResolveModel("openai/gpt-9")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || model != "gpt-9" {
		t.Errorf("got (%s, %s); want (openai, gpt-9)", provider, model)
	}
}

func TestResolveModelLiteralOnDefaultProvider(t *testing.T) {
	app := newResolverApp(t, "openai", map[string]*stubProvider{
		"openai": {id: "openai", defaultModel: "gpt-d", smartModel: "gpt-s", fastModel: "gpt-f"},
	}, nil)

	provider, model, err := app.ResolveModel("custom-model")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || model != "custom-model" {
		t.Errorf("got (%s, %s); want (openai, custom-model)", provider, model)
	}
}

func TestResolveModelCustomAlias(t *testing.T) {
	app := newResolverApp(t, "openai", map[string]*stubProvider{
		"openai":    {id: "openai", defaultModel: "gpt-d", smartModel: "gpt-s", fastModel: "gpt-f"},
		"anthropic": {id: "anthropic", defaultModel: "claude-d", smartModel: "claude-s", fastModel: "claude-f"},
	}, map[string]string{
		"reasoning": "anthropic/claude-s",
		"cheap":     "openai/gpt-f",
	})

	provider, model, err := app.ResolveModel("reasoning")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "anthropic" || model != "claude-s" {
		t.Errorf("got (%s, %s); want (anthropic, claude-s)", provider, model)
	}

	provider, model, err = app.ResolveModel("cheap")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || model != "gpt-f" {
		t.Errorf("got (%s, %s); want (openai, gpt-f)", provider, model)
	}
}

func TestResolveModelCustomAliasOverridesBuiltin(t *testing.T) {
	// User's "smart" alias trumps the built-in tier resolution.
	app := newResolverApp(t, "openai", map[string]*stubProvider{
		"openai":    {id: "openai", defaultModel: "gpt-d", smartModel: "gpt-s", fastModel: "gpt-f"},
		"anthropic": {id: "anthropic", defaultModel: "claude-d", smartModel: "claude-s", fastModel: "claude-f"},
	}, map[string]string{
		"smart": "anthropic/claude-d",
	})

	provider, model, err := app.ResolveModel("smart")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "anthropic" || model != "claude-d" {
		t.Errorf("got (%s, %s); want (anthropic, claude-d)", provider, model)
	}
}

func TestResolveModelNoDefaultPicksFirstProvider(t *testing.T) {
	// Default unset — should pick whichever provider is registered.
	app := newResolverApp(t, "", map[string]*stubProvider{
		"only": {id: "only", defaultModel: "only-d", smartModel: "only-s", fastModel: "only-f"},
	}, nil)

	provider, model, err := app.ResolveModel("smart")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "only" || model != "only-s" {
		t.Errorf("got (%s, %s); want (only, only-s)", provider, model)
	}
}

func TestResolveModelNoProvidersError(t *testing.T) {
	app := New(&Config{}) // no providers at all

	_, _, err := app.ResolveModel("smart")
	if err == nil {
		t.Fatal("expected error when no providers configured")
	}
	if !strings.Contains(err.Error(), "no default provider") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestResolveModelUnknownProviderError(t *testing.T) {
	app := newResolverApp(t, "openai", map[string]*stubProvider{
		"openai": {id: "openai", defaultModel: "gpt-d", smartModel: "gpt-s", fastModel: "gpt-f"},
	}, nil)

	// "anthropic/smart" but no anthropic config registered.
	_, _, err := app.ResolveModel("anthropic/smart")
	if err == nil {
		t.Fatal("expected error for unconfigured provider")
	}
}

// Quick sanity check that GetTextModel surfaces the resolver result.
func TestGetTextModelReturnsResolvedNames(t *testing.T) {
	app := newResolverApp(t, "openai", map[string]*stubProvider{
		"openai": {id: "openai", defaultModel: "gpt-d", smartModel: "gpt-s", fastModel: "gpt-f"},
	}, nil)

	_, provider, model, err := app.GetTextModel("smart")
	if err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || model != "gpt-s" {
		t.Errorf("got (%s, %s); want (openai, gpt-s)", provider, model)
	}
}

// silence "unused" warnings on ctx if the stubProvider helpers ever need it.
var _ = context.Background

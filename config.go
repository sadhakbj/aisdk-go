package aisdk

import (
	"fmt"
	"strings"
	"sync"
)

// Config is the top-level configuration for the SDK.
//
// You typically configure credentials and connection info only.
// Model names are defined by the providers themselves:
//
//	aisdk.Configure(&aisdk.Config{
//	    Providers: map[string]aisdk.ProviderConfig{
//	        "openai":    &openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")},
//	        "anthropic": &anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")},
//	    },
//	    Default: "openai",
//	})
//
// Then use built-in aliases like "smart", "fast", "default" — the provider
// knows which model each tier maps to.
type Config struct {
	// Providers maps provider names to their configuration (typically API keys and base URLs).
	Providers map[string]ProviderConfig

	// Default is the name of the default provider.
	Default string

	// Models is an optional map of custom aliases to "provider/model" strings.
	// Built-in aliases ("smart", "fast", "default") are resolved from the
	// provider automatically — you only need this for custom aliases.
	//
	// Example:
	//   "reasoning": "openai/o1"
	//   "code":      "anthropic/claude-sonnet-4-6"
	Models map[string]string

	// ConversationStore is the store for persisting conversations.
	// If nil, an InMemoryStore is used.
	ConversationStore ConversationStore

	// MaxConversationMessages is the max messages to load from a conversation.
	// Defaults to 100.
	MaxConversationMessages int

	// Retry configures automatic retry behaviour for rate limit and provider
	// overload errors. If nil, defaults are used (3 retries, 500ms initial delay,
	// 30s max delay). Set MaxRetries to 0 to disable retries entirely.
	Retry *RetryConfig
}

// ProviderFactory is a function that creates a Provider from its config.
type ProviderFactory func(config ProviderConfig) (Provider, error)

// --- Provider registry (global) ---

var (
	providerFactories   = map[string]ProviderFactory{}
	providerFactoriesMu sync.RWMutex
)

// RegisterProvider registers a provider factory by name.
// Provider packages call this in their init() function.
func RegisterProvider(name string, factory ProviderFactory) {
	providerFactoriesMu.Lock()
	defer providerFactoriesMu.Unlock()
	providerFactories[name] = factory
}

func getProviderFactory(name string) (ProviderFactory, bool) {
	providerFactoriesMu.RLock()
	defer providerFactoriesMu.RUnlock()
	f, ok := providerFactories[name]
	return f, ok
}

// --- Global default App ---

var (
	defaultApp   *App
	defaultAppMu sync.RWMutex
)

// Configure sets the global default App from the given configuration.
// Call this once at startup (typically from config/ai.go's init function),
// then use package-level functions like GenerateText(), StreamText(), etc.
//
// Call this once at process startup so package-level helpers use this app.
//
//	func init() {
//	    aisdk.Configure(&aisdk.Config{
//	        Providers: map[string]aisdk.ProviderConfig{
//	            "openai": &openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")},
//	        },
//	        Default: "openai",
//	    })
//	}
func Configure(cfg *Config) {
	app := New(cfg)
	defaultAppMu.Lock()
	defer defaultAppMu.Unlock()
	defaultApp = app
}

// ErrNotConfigured is returned by DefaultApp when Configure has not been
// called and no SetDefaultApp override has been provided.
var ErrNotConfigured = fmt.Errorf("aisdk: not configured. Call aisdk.Configure() first, or import your config package (e.g. import _ \"yourapp/config\")")

// DefaultApp returns the global default App, or ErrNotConfigured if Configure
// has not been called.
func DefaultApp() (*App, error) {
	defaultAppMu.RLock()
	defer defaultAppMu.RUnlock()
	if defaultApp == nil {
		return nil, ErrNotConfigured
	}
	return defaultApp, nil
}

// SetDefaultApp sets a custom App as the global default.
// Useful in tests or when you need explicit control.
func SetDefaultApp(app *App) {
	defaultAppMu.Lock()
	defer defaultAppMu.Unlock()
	defaultApp = app
}

// --- App ---

// App is the main entry point for the SDK. It holds configuration,
// resolved providers, and the conversation store.
type App struct {
	config    *Config
	providers map[string]Provider
	store     ConversationStore
	mu        sync.RWMutex
}

// New creates a new App from configuration.
// Most users should use Configure() instead to set the global default.
func New(cfg *Config) *App {
	if cfg.MaxConversationMessages <= 0 {
		cfg.MaxConversationMessages = 100
	}

	store := cfg.ConversationStore
	if store == nil {
		store = NewInMemoryStore()
	}

	app := &App{
		config:    cfg,
		providers: make(map[string]Provider),
		store:     store,
	}

	return app
}

// Config returns the app's configuration.
func (a *App) Config() *Config {
	return a.config
}

// Store returns the conversation store.
func (a *App) Store() ConversationStore {
	return a.store
}

// Provider returns a resolved provider by name. Providers are lazily created
// on first access from their registered factory + config.
func (a *App) Provider(name string) (Provider, error) {
	a.mu.RLock()
	if p, ok := a.providers[name]; ok {
		a.mu.RUnlock()
		return p, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check after acquiring write lock
	if p, ok := a.providers[name]; ok {
		return p, nil
	}

	cfg, ok := a.config.Providers[name]
	if !ok {
		return nil, fmt.Errorf("aisdk: provider %q not configured", name)
	}

	factory, ok := getProviderFactory(name)
	if !ok {
		return nil, fmt.Errorf("aisdk: provider %q not registered (did you import the provider package?)", name)
	}

	p, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("aisdk: failed to create provider %q: %w", name, err)
	}

	a.providers[name] = p
	return p, nil
}

// DefaultProvider returns the default provider.
func (a *App) DefaultProvider() (Provider, error) {
	name := a.config.Default
	if name == "" {
		// If no default set, use the first configured provider
		for n := range a.config.Providers {
			name = n
			break
		}
	}
	if name == "" {
		return nil, fmt.Errorf("aisdk: no providers configured")
	}
	return a.Provider(name)
}

// Built-in model tier aliases, resolved from the provider itself.
const (
	ModelDefault = "default" // Provider's default model
	ModelSmart   = "smart"   // Provider's most capable model
	ModelFast    = "fast"    // Provider's cheapest/fastest model
)

// isBuiltinAlias checks if a model string is a built-in tier alias.
func isBuiltinAlias(model string) bool {
	return model == ModelDefault || model == ModelSmart || model == ModelFast
}

// ResolveModel resolves a model string into (providerName, modelName).
//
// Resolution order:
//  1. "provider/model"  → explicit provider + model
//  2. "provider/smart"  → explicit provider + its smart model
//  3. "smart"           → default provider's smart model (from Provider.SmartModel())
//  4. "fast"            → default provider's fast model (from Provider.FastModel())
//  5. "default"         → default provider's default model (from Provider.DefaultModel())
//  6. Custom aliases    → resolved from Config.Models (optional user overrides)
//  7. "gpt-5.4"         → default provider + literal model name
func (a *App) ResolveModel(model string) (providerName string, modelName string, err error) {
	// 1. Check user-defined aliases first (optional overrides)
	if a.config.Models != nil {
		if alias, ok := a.config.Models[model]; ok {
			model = alias
		}
	}

	// 2. Handle "provider/model" format (including "provider/smart")
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		pName, mName := parts[0], parts[1]

		// If the model part is a built-in alias, resolve from that provider
		if isBuiltinAlias(mName) {
			p, err := a.Provider(pName)
			if err != nil {
				return "", "", err
			}
			return pName, resolveModelTier(p, mName), nil
		}

		return pName, mName, nil
	}

	// 3. Resolve default provider name
	defaultName := a.config.Default
	if defaultName == "" {
		for n := range a.config.Providers {
			defaultName = n
			break
		}
	}
	if defaultName == "" {
		return "", "", fmt.Errorf("aisdk: cannot resolve model %q, no default provider", model)
	}

	// 4. If it's a built-in alias, resolve from the default provider
	if isBuiltinAlias(model) {
		p, err := a.Provider(defaultName)
		if err != nil {
			return "", "", err
		}
		return defaultName, resolveModelTier(p, model), nil
	}

	// 5. Empty model → default provider's default model
	if model == "" {
		p, err := a.Provider(defaultName)
		if err != nil {
			return "", "", err
		}
		return defaultName, p.DefaultModel(), nil
	}

	// 6. Literal model name → use with default provider
	return defaultName, model, nil
}

// resolveModelTier maps a built-in tier alias to the actual model name.
func resolveModelTier(p Provider, tier string) string {
	switch tier {
	case ModelSmart:
		return p.SmartModel()
	case ModelFast:
		return p.FastModel()
	default:
		return p.DefaultModel()
	}
}

// GetTextModel resolves a model string and returns the corresponding TextModel.
func (a *App) GetTextModel(model string) (TextModel, string, string, error) {
	providerName, modelName, err := a.ResolveModel(model)
	if err != nil {
		return nil, "", "", err
	}

	p, err := a.Provider(providerName)
	if err != nil {
		return nil, "", "", err
	}

	return p.TextModel(modelName), providerName, modelName, nil
}

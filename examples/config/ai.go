// Package config configures the AI SDK for all examples.
//
// This is exactly what `aisdk init` generates in a real project.
// Every example imports it with a blank import:
//
//	import _ "github.com/sadhakbj/aisdk-go/examples/config"
package config

import (
	"os"

	"github.com/sadhakbj/aisdk-go"
	"github.com/sadhakbj/aisdk-go/providers/anthropic"
	"github.com/sadhakbj/aisdk-go/providers/openai"
)

func init() {
	aisdk.Configure(&aisdk.Config{
		Providers: map[string]aisdk.ProviderConfig{
			"openai": &openai.Config{
				APIKey:  os.Getenv("OPENAI_API_KEY"),
				BaseURL: "", // Optional: for proxies
			},
			"anthropic": &anthropic.Config{
				APIKey: os.Getenv("ANTHROPIC_API_KEY"),
			},
		},
		Default: "openai",
	})
}

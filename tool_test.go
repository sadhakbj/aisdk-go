package aisdk

import (
	"testing"
)

func TestGenerateJSONSchema(t *testing.T) {
	type Params struct {
		City     string   `json:"city" jsonschema:"required,description=City name"`
		Units    string   `json:"units" jsonschema:"enum=metric|imperial"`
		Days     int      `json:"days" jsonschema:"minimum=1,maximum=7"`
		Tags     []string `json:"tags"`
		Detailed bool     `json:"detailed"`
	}

	schema := GenerateJSONSchema(Params{})

	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties to be a map")
	}

	// Check city field
	city, ok := props["city"].(map[string]any)
	if !ok {
		t.Fatal("expected 'city' property")
	}
	if city["type"] != "string" {
		t.Errorf("expected city type 'string', got %v", city["type"])
	}
	if city["description"] != "City name" {
		t.Errorf("expected city description 'City name', got %v", city["description"])
	}

	// Check units field has enum
	units, ok := props["units"].(map[string]any)
	if !ok {
		t.Fatal("expected 'units' property")
	}
	enumVals, ok := units["enum"].([]string)
	if !ok {
		t.Fatal("expected enum to be a string slice")
	}
	if len(enumVals) != 2 || enumVals[0] != "metric" || enumVals[1] != "imperial" {
		t.Errorf("unexpected enum values: %v", enumVals)
	}

	// Check days field has min/max
	days, ok := props["days"].(map[string]any)
	if !ok {
		t.Fatal("expected 'days' property")
	}
	if days["type"] != "integer" {
		t.Errorf("expected days type 'integer', got %v", days["type"])
	}

	// Check tags is array of strings
	tags, ok := props["tags"].(map[string]any)
	if !ok {
		t.Fatal("expected 'tags' property")
	}
	if tags["type"] != "array" {
		t.Errorf("expected tags type 'array', got %v", tags["type"])
	}

	// Check detailed is boolean
	detailed, ok := props["detailed"].(map[string]any)
	if !ok {
		t.Fatal("expected 'detailed' property")
	}
	if detailed["type"] != "boolean" {
		t.Errorf("expected detailed type 'boolean', got %v", detailed["type"])
	}

	// Check required — all fields are required (OpenAI strict mode)
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("expected required to be a string slice")
	}
	if len(required) != 5 {
		t.Errorf("expected all 5 fields required, got %v", required)
	}

	// Check additionalProperties is false (OpenAI strict mode)
	additionalProps, ok := schema["additionalProperties"].(bool)
	if !ok || additionalProps != false {
		t.Errorf("expected additionalProperties=false, got %v", schema["additionalProperties"])
	}
}

func TestGenerateJSONSchemaEmpty(t *testing.T) {
	schema := GenerateJSONSchema(nil)
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}
}

func TestGenerateJSONSchemaNested(t *testing.T) {
	type Address struct {
		Street string `json:"street"`
		City   string `json:"city"`
	}
	type Person struct {
		Name    string  `json:"name" jsonschema:"required"`
		Age     int     `json:"age"`
		Address Address `json:"address"`
	}

	schema := GenerateJSONSchema(Person{})
	props := schema["properties"].(map[string]any)

	addr, ok := props["address"].(map[string]any)
	if !ok {
		t.Fatal("expected 'address' property")
	}
	if addr["type"] != "object" {
		t.Errorf("expected address type 'object', got %v", addr["type"])
	}

	addrProps, ok := addr["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected address to have properties")
	}
	if _, ok := addrProps["street"]; !ok {
		t.Error("expected address to have 'street' property")
	}
	if _, ok := addrProps["city"]; !ok {
		t.Error("expected address to have 'city' property")
	}
}

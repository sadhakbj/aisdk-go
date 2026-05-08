package aisdk

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// Tool is the interface for defining callable tools that agents can use.
type Tool interface {
	// Name returns the tool's unique name.
	Name() string

	// Description returns a human-readable description of what the tool does.
	Description() string

	// Parameters returns a struct whose fields define the tool's parameters.
	// The struct should use `json` and `jsonschema` tags for schema generation.
	Parameters() any

	// Execute runs the tool with the given JSON arguments and returns a result.
	Execute(ctx context.Context, args json.RawMessage) (any, error)
}

// ToolToDefinition converts a Tool into a ToolDef for sending to providers.
func ToolToDefinition(t Tool) ToolDef {
	return ToolDef{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  GenerateJSONSchema(t.Parameters()),
	}
}

// ToolsToDefinitions converts a slice of Tools to ToolDefs.
func ToolsToDefinitions(tools []Tool) []ToolDef {
	defs := make([]ToolDef, len(tools))
	for i, t := range tools {
		defs[i] = ToolToDefinition(t)
	}
	return defs
}

// FindTool looks up a tool by name from a slice.
func FindTool(tools []Tool, name string) (Tool, bool) {
	for _, t := range tools {
		if t.Name() == name {
			return t, true
		}
	}
	return nil, false
}

// --- JSON Schema generation from Go struct tags ---

// GenerateJSONSchema generates a JSON Schema object from a Go struct.
// Supports `json` tags for field names and `jsonschema` tags for constraints.
//
// Supported jsonschema tag values:
//   - required        marks the field as required
//   - description=... sets the field description
//   - enum=a|b|c      sets allowed values
//   - minimum=N       sets minimum for numbers
//   - maximum=N       sets maximum for numbers
func GenerateJSONSchema(v any) map[string]any {
	if v == nil {
		return emptyObjectSchema()
	}

	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return emptyObjectSchema()
	}

	return generateObjectSchema(t)
}

// emptyObjectSchema returns a strict-mode-compatible JSON Schema for an
// argumentless function. OpenAI's Responses API (strict: true) requires
// every object schema to declare additionalProperties:false and a
// non-null required array, even when there are no properties.
func emptyObjectSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func generateObjectSchema(t reflect.Type) map[string]any {
	properties := map[string]any{}
	// Initialize as empty slice (not nil) so the JSON marshals to [] rather
	// than null when the struct has no fields. OpenAI strict mode rejects
	// "required": null with "None is not of type 'array'".
	required := []string{}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		name := field.Name
		jsonTag := field.Tag.Get("json")
		if jsonTag != "" {
			parts := strings.Split(jsonTag, ",")
			if parts[0] == "-" {
				continue
			}
			if parts[0] != "" {
				name = parts[0]
			}
		}

		prop := generateFieldSchema(field.Type)

		// Parse jsonschema tag
		schemaTag := field.Tag.Get("jsonschema")
		if schemaTag != "" {
			for part := range strings.SplitSeq(schemaTag, ",") {
				part = strings.TrimSpace(part)
				switch {
				case part == "required":
					// Already handled below — all fields are required for strict mode
				case strings.HasPrefix(part, "description="):
					prop["description"] = strings.TrimPrefix(part, "description=")
				case strings.HasPrefix(part, "enum="):
					vals := strings.Split(strings.TrimPrefix(part, "enum="), "|")
					prop["enum"] = vals
				case strings.HasPrefix(part, "minimum="):
					prop["minimum"] = parseNumber(strings.TrimPrefix(part, "minimum="))
				case strings.HasPrefix(part, "maximum="):
					prop["maximum"] = parseNumber(strings.TrimPrefix(part, "maximum="))
				}
			}
		}

		properties[name] = prop

		// OpenAI strict mode requires ALL fields to be in "required".
		required = append(required, name)
	}

	schema := map[string]any{
		"type":                 "object",
		"properties":          properties,
		"required":            required,
		"additionalProperties": false,
	}
	return schema
}

func generateFieldSchema(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Slice, reflect.Array:
		items := generateFieldSchema(t.Elem())
		return map[string]any{"type": "array", "items": items}
	case reflect.Map:
		return map[string]any{"type": "object"}
	case reflect.Struct:
		return generateObjectSchema(t)
	case reflect.Ptr:
		return generateFieldSchema(t.Elem())
	default:
		return map[string]any{"type": "string"}
	}
}

func parseNumber(s string) any {
	var n float64
	_, err := fmt.Sscanf(s, "%f", &n)
	if err != nil {
		return 0
	}
	if n == float64(int(n)) {
		return int(n)
	}
	return n
}

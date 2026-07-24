package goblin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"
)

const (
	// SchemaFileName is the default basename for the installed goblin/v1 JSON Schema.
	SchemaFileName = "goblin-v1.json"
	// SchemaDraft is the JSON Schema meta-schema URI used by yaml-language-server.
	SchemaDraft = "http://json-schema.org/draft-07/schema#"
)

// DefaultSchemaPath returns ${XDG_CONFIG_HOME:-$HOME/.config}/goblin/goblin-v1.json.
func DefaultSchemaPath() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.Getenv("HOME")
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "goblin", SchemaFileName)
}

// JSONSchema builds a draft-07 JSON Schema for goblin/v1 YAML documents from
// the current action registry. Action names become an enum; each action's
// YAML-bindable seed struct fields become conditional with properties.
func JSONSchema() map[string]any {
	actions := All()
	names := make([]string, 0, len(actions))
	defs := make(map[string]any, len(actions))
	conditionals := make([]any, 0, len(actions))
	resultTypes := collectResultTypes(actions)

	for _, a := range actions {
		names = append(names, a.Name())
		withSchema := actionWithSchema(a, resultTypes)
		defName := schemaDefName(a.Name())
		defs[defName] = withSchema
		conditionals = append(conditionals, map[string]any{
			"if": map[string]any{
				"properties": map[string]any{
					"action": map[string]any{"const": a.Name()},
				},
				"required": []string{"action"},
			},
			"then": map[string]any{
				"properties": map[string]any{
					"with": map[string]any{"$ref": "#/$defs/" + defName},
				},
			},
		})
	}

	stepSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"id", "action"},
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Unique step identifier within the flow",
			},
			"action": map[string]any{
				"type":        "string",
				"enum":        names,
				"description": "Registered goblin action name",
			},
			"with": map[string]any{
				"type":                 "object",
				"description":          "Parameters bound onto the action's YAML-friendly seed structs",
				"additionalProperties": true,
			},
		},
	}
	if len(conditionals) > 0 {
		stepSchema["allOf"] = conditionals
	}

	return map[string]any{
		"$schema":              SchemaDraft,
		"$id":                  "https://ruivieira.dev/schemas/goblin-v1.json",
		"title":                "Goblin flow (goblin/v1)",
		"description":          "YAML flow document for goblin.RunYAML",
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"name", "kind", "steps"},
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"minLength":   1,
				"description": "Human-readable flow name",
			},
			"kind": map[string]any{
				"type":        "string",
				"const":       KindV1,
				"description": "Document kind; must be goblin/v1",
			},
			"parameters": map[string]any{
				"type":                 "object",
				"description":          "Top-level parameters referenced as ${name} in step with values",
				"additionalProperties": true,
			},
			"steps": map[string]any{
				"type":        "array",
				"minItems":    1,
				"description": "Ordered list of action invocations (DAG edges come from Go types)",
				"items":       stepSchema,
			},
		},
		"$defs": defs,
	}
}

// WriteJSONSchema encodes JSONSchema() as indented JSON to w.
func WriteJSONSchema(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(JSONSchema()); err != nil {
		return fmt.Errorf("goblin: encode schema: %w", err)
	}
	return nil
}

// InstallJSONSchema writes the schema to path (creating parent directories).
// If path is empty, DefaultSchemaPath() is used.
func InstallJSONSchema(path string) error {
	if path == "" {
		path = DefaultSchemaPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("goblin: create schema dir: %w", err)
	}
	// path is an explicit InstallJSONSchema API argument (caller-controlled).
	f, err := os.Create(path) // #nosec G304 -- caller-provided install path
	if err != nil {
		return fmt.Errorf("goblin: create schema file: %w", err)
	}
	if err := WriteJSONSchema(f); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("goblin: close schema file: %w", err)
	}
	return nil
}

func schemaDefName(action string) string {
	var b strings.Builder
	b.WriteString("with_")
	for _, r := range action {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func actionWithSchema(a Action, resultTypes map[reflect.Type]struct{}) map[string]any {
	props := make(map[string]any)
	fnType := reflect.TypeOf(a.Fn())
	for i := 1; i < fnType.NumIn(); i++ {
		pt := fnType.In(i)
		if pt == depsType {
			continue
		}
		if _, isResult := resultTypes[pt]; isResult {
			continue
		}
		if pt.Kind() != reflect.Struct {
			continue
		}
		for _, field := range exportedYAMLFields(pt) {
			props[field.name] = fieldWithParamRef(field.schema)
		}
	}
	out := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
	}
	if desc := a.Description(); desc != "" {
		out["description"] = desc
	}
	return out
}

type yamlField struct {
	name   string
	schema map[string]any
}

func exportedYAMLFields(t reflect.Type) []yamlField {
	var out []yamlField
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		name, skip := yamlFieldName(f)
		if skip || name == "" {
			continue
		}
		schema, ok := goTypeSchema(f.Type)
		if !ok {
			continue
		}
		out = append(out, yamlField{name: name, schema: schema})
	}
	return out
}

func yamlFieldName(f reflect.StructField) (string, bool) {
	tag := f.Tag.Get("yaml")
	if tag == "-" {
		return "", true
	}
	if tag != "" {
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			return "", true
		}
		return name, false
	}
	// yaml.v3 default: lowercased field name
	return strings.ToLower(f.Name), false
}

func goTypeSchema(t reflect.Type) (map[string]any, bool) {
	for t.Kind() == reflect.Pointer {
		// Pointer seeds / fields are YAML-hostile for autocomplete purposes.
		return nil, false
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}, true
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}, true
	case reflect.Slice, reflect.Array:
		item, ok := goTypeSchema(t.Elem())
		if !ok {
			return map[string]any{"type": "array"}, true
		}
		return map[string]any{"type": "array", "items": item}, true
	case reflect.Map:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		}, true
	case reflect.Struct:
		props := make(map[string]any)
		for _, field := range exportedYAMLFields(t) {
			props[field.name] = field.schema
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           props,
		}, true
	default:
		return nil, false
	}
}

func fieldWithParamRef(typed map[string]any) map[string]any {
	return map[string]any{
		"oneOf": []any{
			typed,
			map[string]any{
				"type":        "string",
				"pattern":     `^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`,
				"description": "Whole-token parameter reference (${name})",
			},
		},
	}
}

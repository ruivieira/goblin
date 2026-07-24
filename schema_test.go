package goblin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruivieira/goblin"
)

type schemaCfg struct {
	URL    string `yaml:"url"`
	Branch string `yaml:"branch"`
}

type schemaOut struct {
	OK bool
}

func TestJSONSchemaIncludesActionAndWith(t *testing.T) {
	if goblin.ByName("schema-test-clone") == nil {
		goblin.Register(goblin.New(goblin.Spec{
			Name:        "schema-test-clone",
			Description: "schema test clone action",
			Fn: func(_ context.Context, deps goblin.Deps, cfg schemaCfg) (schemaOut, error) {
				return schemaOut{OK: true}, nil
			},
		}))
	}

	raw, err := json.Marshal(goblin.JSONSchema())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if schema["$schema"] != goblin.SchemaDraft {
		t.Fatalf("$schema: got %v", schema["$schema"])
	}
	props, _ := schema["properties"].(map[string]any)
	kind, _ := props["kind"].(map[string]any)
	if kind["const"] != goblin.KindV1 {
		t.Fatalf("kind.const: got %v", kind["const"])
	}

	defs, _ := schema["$defs"].(map[string]any)
	withDef, ok := defs["with_schema_test_clone"].(map[string]any)
	if !ok {
		t.Fatalf("missing $defs.with_schema_test_clone; defs=%v", keysOf(defs))
	}
	withProps, _ := withDef["properties"].(map[string]any)
	if _, ok := withProps["url"]; !ok {
		t.Fatalf("expected url in with properties, got %v", keysOf(withProps))
	}
	if _, ok := withProps["branch"]; !ok {
		t.Fatalf("expected branch in with properties, got %v", keysOf(withProps))
	}

	steps, _ := props["steps"].(map[string]any)
	items, _ := steps["items"].(map[string]any)
	itemProps, _ := items["properties"].(map[string]any)
	action, _ := itemProps["action"].(map[string]any)
	enum, _ := action["enum"].([]any)
	found := false
	for _, v := range enum {
		if v == "schema-test-clone" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("action enum missing schema-test-clone: %v", enum)
	}
}

func TestWriteJSONSchemaAndInstall(t *testing.T) {
	if goblin.ByName("schema-test-write") == nil {
		goblin.Register(goblin.New(goblin.Spec{
			Name: "schema-test-write",
			Fn: func(_ context.Context, deps goblin.Deps, cfg schemaCfg) (schemaOut, error) {
				return schemaOut{}, nil
			},
		}))
	}

	var buf bytes.Buffer
	if err := goblin.WriteJSONSchema(&buf); err != nil {
		t.Fatalf("WriteJSONSchema: %v", err)
	}
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("invalid JSON: %s", buf.String())
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "goblin-v1.json")
	if err := goblin.InstallJSONSchema(path); err != nil {
		t.Fatalf("InstallJSONSchema: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("installed file is not valid JSON")
	}
}

func TestDefaultSchemaPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config-test")
	got := goblin.DefaultSchemaPath()
	want := filepath.Join("/tmp/xdg-config-test", "goblin", goblin.SchemaFileName)
	if got != want {
		t.Fatalf("DefaultSchemaPath: got %q want %q", got, want)
	}
}

func TestDefaultSchemaPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/home-fallback-test")
	got := goblin.DefaultSchemaPath()
	want := filepath.Join("/tmp/home-fallback-test", ".config", "goblin", goblin.SchemaFileName)
	if got != want {
		t.Fatalf("DefaultSchemaPath: got %q want %q", got, want)
	}
}

type mixedSchemaCfg struct {
	Name    string            `yaml:"name"`
	Enabled bool              `yaml:"enabled"`
	Count   int               `yaml:"count"`
	Tags    []string          `yaml:"tags"`
	Meta    map[string]string `yaml:"meta"`
	Nested  struct {
		Path string `yaml:"path"`
	} `yaml:"nested"`
	Skip string  `yaml:"-"`
	Ptr  *string `yaml:"ptr"`
}

type mixedSchemaOut struct {
	OK bool
}

func TestJSONSchemaMixedFieldTypes(t *testing.T) {
	if goblin.ByName("schema-test-mixed") == nil {
		goblin.Register(goblin.New(goblin.Spec{
			Name: "schema-test-mixed",
			Fn: func(_ context.Context, deps goblin.Deps, cfg mixedSchemaCfg) (mixedSchemaOut, error) {
				return mixedSchemaOut{OK: true}, nil
			},
		}))
	}

	raw, err := json.Marshal(goblin.JSONSchema())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	defs, _ := schema["$defs"].(map[string]any)
	withDef, ok := defs["with_schema_test_mixed"].(map[string]any)
	if !ok {
		t.Fatalf("missing $defs.with_schema_test_mixed; defs=%v", keysOf(defs))
	}
	withProps, _ := withDef["properties"].(map[string]any)

	assertType := func(field, want string) {
		t.Helper()
		prop, ok := withProps[field].(map[string]any)
		if !ok {
			t.Fatalf("missing field %q in %v", field, keysOf(withProps))
		}
		// Fields are wrapped in oneOf (typed | ${param}).
		oneOf, ok := prop["oneOf"].([]any)
		if !ok || len(oneOf) == 0 {
			t.Fatalf("field %q: expected oneOf, got %#v", field, prop)
		}
		typed, _ := oneOf[0].(map[string]any)
		if typed["type"] != want {
			t.Fatalf("field %q type: got %v want %q", field, typed["type"], want)
		}
	}

	assertType("name", "string")
	assertType("enabled", "boolean")
	assertType("count", "number")
	assertType("tags", "array")
	assertType("meta", "object")
	assertType("nested", "object")

	if _, ok := withProps["ptr"]; ok {
		t.Fatal("pointer field should be omitted from schema")
	}
	if _, ok := withProps["skip"]; ok {
		t.Fatal("yaml:\"-\" field should be omitted from schema")
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

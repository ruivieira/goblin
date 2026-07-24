package goblin_test

import (
	"context"
	"embed"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ruivieira/goblin"
)

//go:embed testdata/*.yaml
var testFlowFS embed.FS

type yamlGreetCfg struct {
	Name string `yaml:"name"`
}

type yamlGreeted struct {
	Msg string
}

type yamlOtherOut struct {
	Y string
}

func registerYAMLGreet(t *testing.T) {
	t.Helper()
	if goblin.ByName("yaml-test-greet") != nil {
		return
	}
	goblin.Register(goblin.New(goblin.Spec{
		Name:        "yaml-test-greet",
		Description: "test greet for YAML flows",
		Fn: func(_ context.Context, deps goblin.Deps, cfg yamlGreetCfg) (yamlGreeted, error) {
			deps.Logger.TaskInfo("yaml-test-greet", "hello "+cfg.Name)
			return yamlGreeted{Msg: "hello " + cfg.Name}, nil
		},
	}))
}

func TestParseAndValidate(t *testing.T) {
	registerYAMLGreet(t)

	doc, err := goblin.Parse([]byte(`
name: ok
kind: goblin/v1
parameters:
  foo: bar
steps:
  - id: s1
    action: yaml-test-greet
    with:
      name: ${foo}
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := goblin.Validate(doc); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsBadKind(t *testing.T) {
	registerYAMLGreet(t)
	doc, err := goblin.Parse([]byte(`
name: bad
kind: goblin/v0
steps:
  - id: s1
    action: yaml-test-greet
`))
	if err != nil {
		t.Fatal(err)
	}
	err = goblin.Validate(doc)
	if err == nil || !strings.Contains(err.Error(), "kind must be") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsUnknownAction(t *testing.T) {
	doc, err := goblin.Parse([]byte(`
name: bad
kind: goblin/v1
steps:
  - id: s1
    action: does-not-exist
`))
	if err != nil {
		t.Fatal(err)
	}
	err = goblin.Validate(doc)
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsDuplicateStepID(t *testing.T) {
	registerYAMLGreet(t)
	doc, err := goblin.Parse([]byte(`
name: bad
kind: goblin/v1
steps:
  - id: s1
    action: yaml-test-greet
  - id: s1
    action: yaml-test-greet
`))
	if err != nil {
		t.Fatal(err)
	}
	err = goblin.Validate(doc)
	if err == nil || !strings.Contains(err.Error(), "duplicate step id") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRejectsMissingParamRef(t *testing.T) {
	registerYAMLGreet(t)
	doc, err := goblin.Parse([]byte(`
name: bad
kind: goblin/v1
steps:
  - id: s1
    action: yaml-test-greet
    with:
      name: ${missing}
`))
	if err != nil {
		t.Fatal(err)
	}
	err = goblin.Validate(doc)
	if err == nil || !strings.Contains(err.Error(), "unknown parameter") {
		t.Fatalf("got %v", err)
	}
}

func TestRunYAMLInterpolatesAndBinds(t *testing.T) {
	registerYAMLGreet(t)
	logger := &goblin.Logger{}
	doc, err := goblin.Parse([]byte(`
name: greet-run
kind: goblin/v1
parameters:
  who: world
steps:
  - id: hello
    action: yaml-test-greet
    with:
      name: ${who}
`))
	if err != nil {
		t.Fatal(err)
	}

	out := captureOutput(t, func() {
		if err := goblin.RunYAML(context.Background(), logger, doc); err != nil {
			t.Fatalf("RunYAML: %v", err)
		}
	})
	if logger.Flow != "greet-run" {
		t.Fatalf("Flow = %q", logger.Flow)
	}
	if !strings.Contains(out, "Task run 'yaml-test-greet'") {
		t.Fatalf("expected task log, got: %q", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Fatalf("expected interpolated name, got: %q", out)
	}
}

func TestRunYAMLDuplicateSeedType(t *testing.T) {
	if goblin.ByName("yaml-test-other") == nil {
		goblin.Register(goblin.New(goblin.Spec{
			Name: "yaml-test-other",
			Fn: func(_ context.Context, deps goblin.Deps, cfg yamlGreetCfg) (yamlOtherOut, error) {
				return yamlOtherOut{Y: cfg.Name}, nil
			},
		}))
	}
	registerYAMLGreet(t)

	doc, err := goblin.Parse([]byte(`
name: dup-seed
kind: goblin/v1
steps:
  - id: a
    action: yaml-test-greet
    with:
      name: one
  - id: b
    action: yaml-test-other
    with:
      name: two
`))
	if err != nil {
		t.Fatal(err)
	}
	err = goblin.RunYAML(context.Background(), &goblin.Logger{Flow: "dup"}, doc)
	if err == nil || !strings.Contains(err.Error(), "provided by multiple steps") {
		t.Fatalf("got %v", err)
	}
}

func TestParseFSAndRunYAMLFS(t *testing.T) {
	registerYAMLGreet(t)

	doc, err := goblin.ParseFS(testFlowFS, "testdata/greet.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "greet-flow" {
		t.Fatalf("Name = %q", doc.Name)
	}
	if err := goblin.Validate(doc); err != nil {
		t.Fatal(err)
	}

	logger := &goblin.Logger{}
	out := captureOutput(t, func() {
		if err := goblin.RunYAMLFS(context.Background(), logger, testFlowFS, "testdata/greet.yaml"); err != nil {
			t.Fatalf("RunYAMLFS: %v", err)
		}
	})
	if !strings.Contains(out, "hello goblin") {
		t.Fatalf("expected greet output, got: %q", out)
	}
}

func TestParseFile(t *testing.T) {
	registerYAMLGreet(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "flow.yaml")
	data, err := os.ReadFile("testdata/greet.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}
	doc, err := goblin.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Name != "greet-flow" {
		t.Fatalf("Name = %q", doc.Name)
	}

	logger := &goblin.Logger{}
	out := captureOutput(t, func() {
		if err := goblin.RunYAMLFile(context.Background(), logger, path); err != nil {
			t.Fatalf("RunYAMLFile: %v", err)
		}
	})
	if !strings.Contains(out, "hello goblin") {
		t.Fatalf("expected greet output, got: %q", out)
	}
}

func TestParseFileMissing(t *testing.T) {
	_, err := goblin.ParseFile("/nonexistent/goblin-flow.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "no such file") {
		t.Logf("got expected failure: %v", err)
	}
}

func TestParseFSMissing(t *testing.T) {
	fsys := fstest.MapFS{}
	_, err := goblin.ParseFS(fsys, "missing.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
}

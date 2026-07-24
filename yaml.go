package goblin

import (
	"fmt"
	"io/fs"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

const KindV1 = "goblin/v1"

// Document is a goblin YAML flow definition.
type Document struct {
	Name       string         `yaml:"name"`
	Kind       string         `yaml:"kind"`
	Parameters map[string]any `yaml:"parameters"`
	Steps      []Step         `yaml:"steps"`
}

// Step is one named action invocation in a Document.
type Step struct {
	ID     string         `yaml:"id"`
	Action string         `yaml:"action"`
	With   map[string]any `yaml:"with"`
}

var paramRefRE = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// Parse unmarshals a goblin YAML document from bytes.
func Parse(data []byte) (*Document, error) {
	var doc Document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("goblin: parse yaml: %w", err)
	}
	if doc.Parameters == nil {
		doc.Parameters = map[string]any{}
	}
	for i := range doc.Steps {
		if doc.Steps[i].With == nil {
			doc.Steps[i].With = map[string]any{}
		}
	}
	return &doc, nil
}

// ParseFile reads path from disk and parses it.
func ParseFile(path string) (*Document, error) {
	// path is an explicit ParseFile API argument (caller-controlled).
	data, err := os.ReadFile(path) // #nosec G304 -- caller-provided flow path
	if err != nil {
		return nil, fmt.Errorf("goblin: read %s: %w", path, err)
	}
	return Parse(data)
}

// ParseFS reads name from fsys (e.g. embed.FS) and parses it.
func ParseFS(fsys fs.FS, name string) (*Document, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("goblin: read %s: %w", name, err)
	}
	return Parse(data)
}

// Validate checks document structure, registered actions, and ${param} refs.
func Validate(doc *Document) error {
	if doc == nil {
		return fmt.Errorf("goblin: document is nil")
	}
	if doc.Kind != KindV1 {
		return fmt.Errorf("goblin: kind must be %q, got %q", KindV1, doc.Kind)
	}
	if doc.Name == "" {
		return fmt.Errorf("goblin: name is required")
	}
	if len(doc.Steps) == 0 {
		return fmt.Errorf("goblin: at least one step is required")
	}

	seenIDs := make(map[string]struct{}, len(doc.Steps))
	for i, step := range doc.Steps {
		if step.ID == "" {
			return fmt.Errorf("goblin: step[%d]: id is required", i)
		}
		if _, dup := seenIDs[step.ID]; dup {
			return fmt.Errorf("goblin: duplicate step id %q", step.ID)
		}
		seenIDs[step.ID] = struct{}{}
		if step.Action == "" {
			return fmt.Errorf("goblin: step %q: action is required", step.ID)
		}
		if ByName(step.Action) == nil {
			return fmt.Errorf("goblin: step %q: unknown action %q", step.ID, step.Action)
		}
		if err := validateWithRefs(step.ID, step.With, doc.Parameters); err != nil {
			return err
		}
	}
	return nil
}

func validateWithRefs(stepID string, with map[string]any, params map[string]any) error {
	for key, val := range with {
		s, ok := val.(string)
		if !ok {
			continue
		}
		m := paramRefRE.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		name := m[1]
		if _, exists := params[name]; !exists {
			return fmt.Errorf("goblin: step %q: with.%s references unknown parameter %q", stepID, key, name)
		}
	}
	return nil
}

// interpolateWith replaces whole-token ${name} values with parameter values.
// The returned map is a shallow copy; nested maps are not deep-copied.
func interpolateWith(with map[string]any, params map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(with))
	for key, val := range with {
		s, ok := val.(string)
		if !ok {
			out[key] = val
			continue
		}
		m := paramRefRE.FindStringSubmatch(s)
		if m == nil {
			out[key] = val
			continue
		}
		name := m[1]
		p, exists := params[name]
		if !exists {
			return nil, fmt.Errorf("unknown parameter %q", name)
		}
		out[key] = p
	}
	return out, nil
}

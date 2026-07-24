package goblin

import (
	"context"
	"fmt"
	"io/fs"
	"reflect"

	"gopkg.in/yaml.v3"
)

var depsType = reflect.TypeOf(Deps{})

// RunYAML validates doc, interpolates ${params}, binds step with maps onto
// typed seeds, and executes via Run. Actions must already be registered.
func RunYAML(ctx context.Context, logger *Logger, doc *Document) error {
	if err := Validate(doc); err != nil {
		return err
	}
	if logger == nil {
		return fmt.Errorf("goblin: logger is required")
	}
	if logger.Flow == "" {
		logger.Flow = doc.Name
	}

	actions := make([]Action, len(doc.Steps))
	for i, step := range doc.Steps {
		actions[i] = ByName(step.Action)
	}

	resultTypes := collectResultTypes(actions)
	seedsByType := make(map[reflect.Type]any)
	inputs := []any{Deps{Logger: logger}}
	steps := make([]any, len(actions))

	for i, a := range actions {
		steps[i] = a
		fnType := reflect.TypeOf(a.Fn())
		with, err := interpolateWith(doc.Steps[i].With, doc.Parameters)
		if err != nil {
			return fmt.Errorf("goblin: step %q: %w", doc.Steps[i].ID, err)
		}
		for j := 1; j < fnType.NumIn(); j++ {
			pt := fnType.In(j)
			if pt == depsType {
				continue
			}
			if _, isResult := resultTypes[pt]; isResult {
				continue
			}
			seed, err := bindSeed(pt, with)
			if err != nil {
				return fmt.Errorf("goblin: step %q: bind %v: %w", doc.Steps[i].ID, pt, err)
			}
			if prev, exists := seedsByType[pt]; exists {
				return fmt.Errorf("goblin: seed type %v provided by multiple steps (duplicate Provide); first %#v", pt, prev)
			}
			seedsByType[pt] = seed
			inputs = append(inputs, seed)
		}
	}

	return Run(ctx, logger, inputs, steps, nil)
}

// RunYAMLFS parses, validates, and runs a YAML flow from fsys.
func RunYAMLFS(ctx context.Context, logger *Logger, fsys fs.FS, name string) error {
	doc, err := ParseFS(fsys, name)
	if err != nil {
		return err
	}
	return RunYAML(ctx, logger, doc)
}

// RunYAMLFile parses, validates, and runs a YAML flow from a filesystem path.
func RunYAMLFile(ctx context.Context, logger *Logger, path string) error {
	doc, err := ParseFile(path)
	if err != nil {
		return err
	}
	return RunYAML(ctx, logger, doc)
}

func collectResultTypes(actions []Action) map[reflect.Type]struct{} {
	out := make(map[reflect.Type]struct{}, len(actions))
	for _, a := range actions {
		fnType := reflect.TypeOf(a.Fn())
		out[fnType.Out(0)] = struct{}{}
	}
	return out
}

func bindSeed(seedType reflect.Type, with map[string]any) (any, error) {
	raw, err := yaml.Marshal(with)
	if err != nil {
		return nil, fmt.Errorf("marshal with: %w", err)
	}
	ptr := reflect.New(seedType)
	if err := yaml.Unmarshal(raw, ptr.Interface()); err != nil {
		return nil, fmt.Errorf("unmarshal into %v: %w", seedType, err)
	}
	return ptr.Elem().Interface(), nil
}

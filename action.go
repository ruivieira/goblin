package goblin

import (
	"context"
	"fmt"
	"reflect"
)

// Action is a named DAG step. Fn must be func(context.Context, ...) (R, error).
type Action interface {
	Name() string
	Description() string
	Fn() any
}

// Validator is an optional Action that validates deps before Fn runs.
// ValidateFn must be func(context.Context, same deps as Fn...) error.
type Validator interface {
	ValidateFn() any
}

// Compensator is an optional Action that undoes a successful Fn on later failure.
// CompensateFn must be func(context.Context, R) error where R is Fn's result type.
type Compensator interface {
	CompensateFn() any
}

// Spec is the primary authoring API for Actions.
type Spec struct {
	Name        string
	Description string
	Fn          any // required: func(context.Context, ...) (R, error)
	Validate    any // optional: func(context.Context, same deps...) error
	Compensate  any // optional: func(context.Context, R) error
}

type specAction struct {
	name        string
	description string
	fn          any
	validate    any
	compensate  any
}

// New builds an Action from Spec. Panics if Name/Fn are missing or signatures are invalid.
func New(s Spec) Action {
	if s.Name == "" {
		panic("goblin: Spec.Name is required")
	}
	if s.Fn == nil {
		panic(fmt.Sprintf("goblin: Spec.Fn is required for action %q", s.Name))
	}
	fnType, err := checkStepFn(s.Fn)
	if err != nil {
		panic(fmt.Sprintf("goblin: action %q Fn: %v", s.Name, err))
	}
	if s.Validate != nil {
		if err := checkValidateFn(s.Validate, fnType); err != nil {
			panic(fmt.Sprintf("goblin: action %q Validate: %v", s.Name, err))
		}
	}
	if s.Compensate != nil {
		if err := checkCompensateFn(s.Compensate, fnType.Out(0)); err != nil {
			panic(fmt.Sprintf("goblin: action %q Compensate: %v", s.Name, err))
		}
	}
	return &specAction{
		name:        s.Name,
		description: s.Description,
		fn:          s.Fn,
		validate:    s.Validate,
		compensate:  s.Compensate,
	}
}

func (a *specAction) Name() string        { return a.name }
func (a *specAction) Description() string { return a.description }
func (a *specAction) Fn() any             { return a.fn }

func (a *specAction) ValidateFn() any {
	return a.validate
}

func (a *specAction) CompensateFn() any {
	return a.compensate
}

var (
	ctxType   = reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType = reflect.TypeOf((*error)(nil)).Elem()
)

func checkStepFn(fn any) (reflect.Type, error) {
	t := reflect.TypeOf(fn)
	if t == nil || t.Kind() != reflect.Func {
		return nil, fmt.Errorf("not a function")
	}
	if t.NumOut() != 2 || !t.Out(1).Implements(errorType) {
		return nil, fmt.Errorf("must have signature func(context.Context, ...) (R, error)")
	}
	if t.NumIn() < 1 || t.In(0) != ctxType {
		return nil, fmt.Errorf("must have signature func(context.Context, ...) (R, error)")
	}
	return t, nil
}

func checkValidateFn(fn any, stepType reflect.Type) error {
	t := reflect.TypeOf(fn)
	if t == nil || t.Kind() != reflect.Func {
		return fmt.Errorf("not a function")
	}
	if t.NumOut() != 1 || !t.Out(0).Implements(errorType) {
		return fmt.Errorf("must return error")
	}
	if t.NumIn() != stepType.NumIn() {
		return fmt.Errorf("must take the same parameters as Fn")
	}
	for i := 0; i < t.NumIn(); i++ {
		if t.In(i) != stepType.In(i) {
			return fmt.Errorf("parameter %d type %v does not match Fn %v", i, t.In(i), stepType.In(i))
		}
	}
	return nil
}

func checkCompensateFn(fn any, resultType reflect.Type) error {
	t := reflect.TypeOf(fn)
	if t == nil || t.Kind() != reflect.Func {
		return fmt.Errorf("not a function")
	}
	if t.NumIn() != 2 || t.In(0) != ctxType || t.In(1) != resultType {
		return fmt.Errorf("must have signature func(context.Context, %v) error", resultType)
	}
	if t.NumOut() != 1 || !t.Out(0).Implements(errorType) {
		return fmt.Errorf("must return error")
	}
	return nil
}

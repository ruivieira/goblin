package goblin

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

// completedStep records a successful Action result for reverse compensation.
type completedStep struct {
	action Action
	result any
}

type runTracker struct {
	mu        sync.Mutex
	completed []completedStep
}

func (t *runTracker) record(a Action, result any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.completed = append(t.completed, completedStep{action: a, result: result})
}

func (t *runTracker) snapshot() []completedStep {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]completedStep, len(t.completed))
	copy(out, t.completed)
	return out
}

// wrapAction returns a dagfunc-compatible function that validates, executes,
// logs via Do/DoValue semantics, and records success for compensation.
func wrapAction(logger *Logger, a Action, tracker *runTracker) (any, error) {
	fn := a.Fn()
	fnType, err := checkStepFn(fn)
	if err != nil {
		return nil, fmt.Errorf("action %q: %w", a.Name(), err)
	}
	fnVal := reflect.ValueOf(fn)

	var validateVal reflect.Value
	if v, ok := a.(Validator); ok {
		if vf := v.ValidateFn(); vf != nil {
			if err := checkValidateFn(vf, fnType); err != nil {
				return nil, fmt.Errorf("action %q ValidateFn: %w", a.Name(), err)
			}
			validateVal = reflect.ValueOf(vf)
		}
	}

	name := a.Name()
	wrapper := reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		if validateVal.IsValid() {
			vout := validateVal.Call(args)
			if errVal := vout[0]; !errVal.IsNil() {
				err := errVal.Interface().(error)
				logger.TaskError(name, fmt.Sprintf("Finished in state Failed('%v')", err))
				logger.flushTask(name, true)
				return []reflect.Value{
					reflect.Zero(fnType.Out(0)),
					reflect.ValueOf(err),
				}
			}
		}

		var outResult reflect.Value
		doErr := Do(logger, name, func() error {
			outs := fnVal.Call(args)
			if errVal := outs[1]; !errVal.IsNil() {
				return errVal.Interface().(error)
			}
			outResult = outs[0]
			return nil
		})

		if doErr != nil {
			return []reflect.Value{
				reflect.Zero(fnType.Out(0)),
				reflect.ValueOf(doErr),
			}
		}
		tracker.record(a, outResult.Interface())
		return []reflect.Value{
			outResult,
			reflect.Zero(errorType),
		}
	})
	return wrapper.Interface(), nil
}

func compensateCompleted(ctx context.Context, logger *Logger, completed []completedStep) {
	for i := len(completed) - 1; i >= 0; i-- {
		step := completed[i]
		c, ok := step.action.(Compensator)
		if !ok {
			continue
		}
		cf := c.CompensateFn()
		if cf == nil {
			continue
		}
		name := step.action.Name() + ":compensate"
		err := Do(logger, name, func() error {
			outs := reflect.ValueOf(cf).Call([]reflect.Value{
				reflect.ValueOf(ctx),
				reflect.ValueOf(step.result),
			})
			if errVal := outs[0]; !errVal.IsNil() {
				return errVal.Interface().(error)
			}
			return nil
		})
		if err != nil {
			logger.FlowError(fmt.Sprintf("compensate %q: %v", step.action.Name(), err))
		}
	}
}

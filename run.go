package goblin

import (
	"context"
	"fmt"

	"github.com/jizhuozhi/go-future/dagfunc"
)

// Run compiles and executes a type-driven DAG.
// inputs are registered via Provide and passed to Compile with concrete values.
// steps may be Action values or raw task functions (func(context.Context, ...) (R, error)).
// Actions are wrapped for validate → execute → auto-log; on failure, Compensators
// run in reverse order of successful completion.
// Run notifies Logger.Stores (default: ConsoleStore) of run/task lifecycle events.
func Run(ctx context.Context, logger *Logger, inputs []any, steps []any, onSuccess func(map[any]any)) error {
	if logger == nil {
		return fmt.Errorf("goblin: logger is required")
	}
	logger.beginRun()

	builder := dagfunc.New()
	for _, in := range inputs {
		if err := builder.Provide(in); err != nil {
			logger.endRun(RunStatusFailed, err)
			return fmt.Errorf("goblin: provide %T: %w", in, err)
		}
	}

	tracker := &runTracker{}
	for _, step := range steps {
		fn := step
		if a, ok := step.(Action); ok {
			wrapped, err := wrapAction(logger, a, tracker)
			if err != nil {
				logger.endRun(RunStatusFailed, err)
				return fmt.Errorf("goblin: wrap action %q: %w", a.Name(), err)
			}
			fn = wrapped
		}
		if err := builder.Use(fn); err != nil {
			logger.endRun(RunStatusFailed, err)
			return fmt.Errorf("goblin: register step: %w", err)
		}
	}

	prog, err := builder.Compile(inputs)
	if err != nil {
		logger.endRun(RunStatusFailed, err)
		return fmt.Errorf("goblin: compile: %w", err)
	}

	results, err := prog.Run(ctx)
	if err != nil {
		compensateCompleted(ctx, logger, tracker.snapshot())
		logger.endRun(RunStatusFailed, err)
		return err
	}

	if onSuccess != nil {
		onSuccess(results)
	}
	logger.endRun(RunStatusCompleted, nil)
	return nil
}

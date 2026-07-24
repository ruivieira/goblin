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
func Run(ctx context.Context, logger *Logger, inputs []any, steps []any, onSuccess func(map[any]any)) error {
	builder := dagfunc.New()
	for _, in := range inputs {
		if err := builder.Provide(in); err != nil {
			return fmt.Errorf("goblin: provide %T: %w", in, err)
		}
	}

	tracker := &runTracker{}
	for _, step := range steps {
		fn := step
		if a, ok := step.(Action); ok {
			wrapped, err := wrapAction(logger, a, tracker)
			if err != nil {
				return fmt.Errorf("goblin: wrap action %q: %w", a.Name(), err)
			}
			fn = wrapped
		}
		if err := builder.Use(fn); err != nil {
			return fmt.Errorf("goblin: register step: %w", err)
		}
	}

	prog, err := builder.Compile(inputs)
	if err != nil {
		return fmt.Errorf("goblin: compile: %w", err)
	}

	results, err := prog.Run(ctx)
	if err != nil {
		compensateCompleted(ctx, logger, tracker.snapshot())
		logger.FlowError("Finished in state Failed()")
		return err
	}

	if onSuccess != nil {
		onSuccess(results)
	}
	logger.FlowInfo("Finished in state Completed()")
	return nil
}

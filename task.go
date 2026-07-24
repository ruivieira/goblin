package goblin

import "fmt"

// Do runs fn with task logging. All messages buffered during fn are emitted
// together with the final state in a single grouped log line.
func Do(logger *Logger, name string, fn func() error) error {
	if err := fn(); err != nil {
		logger.TaskError(name, fmt.Sprintf("Finished in state Failed('%v')", err))
		logger.flushTask(name, true)
		return err
	}
	logger.TaskInfo(name, "Finished in state Completed()")
	logger.flushTask(name, false)
	return nil
}

// DoValue runs fn with task logging and returns its value on success.
func DoValue[T any](logger *Logger, name string, fn func() (T, error)) (T, error) {
	var zero T
	v, err := fn()
	if err != nil {
		logger.TaskError(name, fmt.Sprintf("Finished in state Failed('%v')", err))
		logger.flushTask(name, true)
		return zero, err
	}
	logger.TaskInfo(name, "Finished in state Completed()")
	logger.flushTask(name, false)
	return v, nil
}

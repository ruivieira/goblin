package goblin

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/pterm/pterm"
)

// Logger emits structured pterm log lines for flows and tasks.
// Task messages are buffered and flushed together when the task finishes,
// so all messages for a given task appear under a single header line.
type Logger struct {
	Flow    string
	mu      sync.Mutex
	taskBuf map[string][]string
}

var (
	globalOutMu sync.RWMutex
	globalOut   io.Writer
)

// SetOutput redirects all Logger output to w. Pass nil to restore os.Stdout.
func SetOutput(w io.Writer) {
	globalOutMu.Lock()
	globalOut = w
	globalOutMu.Unlock()
}

func effectiveOutput() io.Writer {
	globalOutMu.RLock()
	w := globalOut
	globalOutMu.RUnlock()
	if w != nil {
		return w
	}
	return os.Stdout
}

// FlowInfo logs a flow-level info message.
func (l *Logger) FlowInfo(msg string) {
	log := l.logger()
	log.Info(fmt.Sprintf("Flow run '%s'", l.Flow), log.Args("message", msg))
}

// FlowError logs a flow-level error message.
func (l *Logger) FlowError(msg string) {
	log := l.logger()
	log.Error(fmt.Sprintf("Flow run '%s'", l.Flow), log.Args("message", msg))
}

// TaskInfo buffers a task-level info message; flushed by Do/DoValue on completion.
func (l *Logger) TaskInfo(task, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.taskBuf == nil {
		l.taskBuf = make(map[string][]string)
	}
	l.taskBuf[task] = append(l.taskBuf[task], msg)
}

// TaskError buffers a task-level error message; flushed by Do/DoValue on completion.
func (l *Logger) TaskError(task, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.taskBuf == nil {
		l.taskBuf = make(map[string][]string)
	}
	l.taskBuf[task] = append(l.taskBuf[task], msg)
}

// flushTask drains the buffer for task and emits a single grouped log line.
func (l *Logger) flushTask(task string, isError bool) {
	l.mu.Lock()
	msgs := l.taskBuf[task]
	delete(l.taskBuf, task)
	l.mu.Unlock()

	ptermLog := l.logger()
	args := make([]any, 0, len(msgs)*2)
	for _, msg := range msgs {
		args = append(args, "message", msg)
	}
	if isError {
		ptermLog.Error(fmt.Sprintf("Task run '%s'", task), ptermLog.Args(args...))
	} else {
		ptermLog.Info(fmt.Sprintf("Task run '%s'", task), ptermLog.Args(args...))
	}
}

func (l *Logger) logger() *pterm.Logger {
	return pterm.DefaultLogger.WithLevel(pterm.LogLevelInfo).WithWriter(effectiveOutput())
}

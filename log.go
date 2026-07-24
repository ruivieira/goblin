package goblin

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/pterm/pterm"
)

// Logger buffers task messages and dispatches run lifecycle events to Stores.
// When Stores is nil or empty, a ConsoleStore is used (pterm output).
type Logger struct {
	Flow   string
	Stores []RunStore

	mu          sync.Mutex
	taskBuf     map[string][]string
	runID       string
	runStarted  time.Time
	taskStarted map[string]time.Time
}

var (
	globalOutMu sync.RWMutex
	globalOut   io.Writer
)

// SetOutput redirects all ConsoleStore / pterm output to w. Pass nil to restore os.Stdout.
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

func newRunID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// FlowInfo logs a flow-level info message via any FlowMessenger stores (Console by default).
func (l *Logger) FlowInfo(msg string) {
	l.dispatchFlowMessage(false, msg)
}

// FlowError logs a flow-level error message via any FlowMessenger stores (Console by default).
func (l *Logger) FlowError(msg string) {
	l.dispatchFlowMessage(true, msg)
}

func (l *Logger) dispatchFlowMessage(isError bool, msg string) {
	emitted := false
	for _, s := range l.effectiveStores() {
		if fm, ok := s.(FlowMessenger); ok {
			fm.FlowMessage(l.Flow, isError, msg)
			emitted = true
		}
	}
	if !emitted {
		defaultConsoleStore.FlowMessage(l.Flow, isError, msg)
	}
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

// flushTask drains the buffer and ends the task via stores (Console emits pterm).
func (l *Logger) flushTask(task string, isError bool) {
	errMsg := ""
	if isError {
		errMsg = "failed"
	}
	l.endTask(task, isError, errMsg)
}

func ptermLogger() *pterm.Logger {
	return pterm.DefaultLogger.WithLevel(pterm.LogLevelInfo).WithWriter(effectiveOutput())
}

func emitFlowPterm(flow string, isError bool, msg string) {
	log := ptermLogger()
	if isError {
		log.Error(fmt.Sprintf("Flow run '%s'", flow), log.Args("message", msg))
	} else {
		log.Info(fmt.Sprintf("Flow run '%s'", flow), log.Args("message", msg))
	}
}

func emitTaskPterm(task string, isError bool, messages []string) {
	log := ptermLogger()
	args := make([]any, 0, len(messages)*2)
	for _, msg := range messages {
		args = append(args, "message", msg)
	}
	if isError {
		log.Error(fmt.Sprintf("Task run '%s'", task), log.Args(args...))
	} else {
		log.Info(fmt.Sprintf("Task run '%s'", task), log.Args(args...))
	}
}

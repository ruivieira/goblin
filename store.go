package goblin

import (
	"errors"
	"fmt"
	"time"
)

// RunStatus is the lifecycle state of a flow or task run.
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

// RunRecord describes one flow execution for storage adapters.
type RunRecord struct {
	ID         string
	Flow       string
	Status     RunStatus
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
}

// TaskRecord describes one task execution within a flow run.
type TaskRecord struct {
	RunID      string
	Task       string
	Status     RunStatus
	Messages   []string
	Error      string
	StartedAt  time.Time
	FinishedAt time.Time
}

// RunStore persists or emits flow/task run information.
// Implementations must be safe for concurrent use from a single Run.
type RunStore interface {
	BeginRun(r RunRecord) error
	EndRun(r RunRecord) error
	BeginTask(t TaskRecord) error
	EndTask(t TaskRecord) error
	Close() error
}

// FlowMessenger is an optional RunStore extension for ad-hoc flow messages
// (e.g. compensate errors) that are not Begin/End lifecycle events.
type FlowMessenger interface {
	FlowMessage(flow string, isError bool, msg string)
}

// MultiStore fans out to all stores. It attempts every store and returns the
// first error encountered (subsequent errors are joined).
func MultiStore(stores ...RunStore) RunStore {
	cp := make([]RunStore, 0, len(stores))
	for _, s := range stores {
		if s != nil {
			cp = append(cp, s)
		}
	}
	return &multiStore{stores: cp}
}

type multiStore struct {
	stores []RunStore
}

func (m *multiStore) BeginRun(r RunRecord) error {
	return m.each(func(s RunStore) error { return s.BeginRun(r) })
}
func (m *multiStore) EndRun(r RunRecord) error {
	return m.each(func(s RunStore) error { return s.EndRun(r) })
}
func (m *multiStore) BeginTask(t TaskRecord) error {
	return m.each(func(s RunStore) error { return s.BeginTask(t) })
}
func (m *multiStore) EndTask(t TaskRecord) error {
	return m.each(func(s RunStore) error { return s.EndTask(t) })
}

func (m *multiStore) Close() error {
	return m.each(func(s RunStore) error { return s.Close() })
}

func (m *multiStore) FlowMessage(flow string, isError bool, msg string) {
	for _, s := range m.stores {
		if fm, ok := s.(FlowMessenger); ok {
			fm.FlowMessage(flow, isError, msg)
		}
	}
}

func (m *multiStore) each(fn func(RunStore) error) error {
	var errs []error
	for _, s := range m.stores {
		if err := fn(s); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (l *Logger) dispatchStore(fn func(RunStore) error) {
	for _, s := range l.effectiveStores() {
		if err := fn(s); err != nil {
			l.emitStoreErr(err)
		}
	}
}

func (l *Logger) emitStoreErr(err error) {
	for _, s := range l.effectiveStores() {
		if fm, ok := s.(FlowMessenger); ok {
			fm.FlowMessage(l.Flow, true, fmt.Sprintf("run store: %v", err))
			return
		}
	}
}

func (l *Logger) effectiveStores() []RunStore {
	if len(l.Stores) == 0 {
		return []RunStore{defaultConsoleStore}
	}
	return l.Stores
}

func (l *Logger) beginRun() RunRecord {
	now := time.Now().UTC()
	id := newRunID()
	l.mu.Lock()
	l.runID = id
	l.runStarted = now
	if l.taskStarted == nil {
		l.taskStarted = make(map[string]time.Time)
	}
	l.mu.Unlock()

	rec := RunRecord{
		ID:        id,
		Flow:      l.Flow,
		Status:    RunStatusRunning,
		StartedAt: now,
	}
	l.dispatchStore(func(s RunStore) error { return s.BeginRun(rec) })
	return rec
}

func (l *Logger) endRun(status RunStatus, runErr error) {
	now := time.Now().UTC()
	l.mu.Lock()
	rec := RunRecord{
		ID:         l.runID,
		Flow:       l.Flow,
		Status:     status,
		StartedAt:  l.runStarted,
		FinishedAt: now,
	}
	l.mu.Unlock()
	if runErr != nil {
		rec.Error = runErr.Error()
	}
	l.dispatchStore(func(s RunStore) error { return s.EndRun(rec) })
}

func (l *Logger) beginTask(name string) {
	now := time.Now().UTC()
	l.mu.Lock()
	runID := l.runID
	if l.taskStarted == nil {
		l.taskStarted = make(map[string]time.Time)
	}
	l.taskStarted[name] = now
	l.mu.Unlock()

	rec := TaskRecord{
		RunID:     runID,
		Task:      name,
		Status:    RunStatusRunning,
		StartedAt: now,
	}
	l.dispatchStore(func(s RunStore) error { return s.BeginTask(rec) })
}

func (l *Logger) endTask(name string, isError bool, errMsg string) {
	now := time.Now().UTC()
	l.mu.Lock()
	msgs := l.taskBuf[name]
	delete(l.taskBuf, name)
	started := l.taskStarted[name]
	delete(l.taskStarted, name)
	runID := l.runID
	l.mu.Unlock()

	status := RunStatusCompleted
	if isError {
		status = RunStatusFailed
	}
	rec := TaskRecord{
		RunID:      runID,
		Task:       name,
		Status:     status,
		Messages:   msgs,
		Error:      errMsg,
		StartedAt:  started,
		FinishedAt: now,
	}
	l.dispatchStore(func(s RunStore) error { return s.EndTask(rec) })
}

package goblin

// ConsoleStore emits run info as pterm structured log lines (the default store).
type ConsoleStore struct{}

// NewConsoleStore returns a console (pterm) run store.
func NewConsoleStore() *ConsoleStore {
	return &ConsoleStore{}
}

var defaultConsoleStore = NewConsoleStore()

// BeginRun is a no-op for console (start is implicit).
func (c *ConsoleStore) BeginRun(_ RunRecord) error { return nil }

// EndRun emits a flow-level Completed/Failed line.
func (c *ConsoleStore) EndRun(r RunRecord) error {
	msg := "Finished in state Completed()"
	isError := false
	if r.Status == RunStatusFailed {
		msg = "Finished in state Failed()"
		isError = true
	}
	emitFlowPterm(r.Flow, isError, msg)
	return nil
}

// BeginTask is a no-op for console (messages are buffered until EndTask).
func (c *ConsoleStore) BeginTask(_ TaskRecord) error { return nil }

// EndTask emits a grouped task log line with buffered messages.
func (c *ConsoleStore) EndTask(t TaskRecord) error {
	emitTaskPterm(t.Task, t.Status == RunStatusFailed, t.Messages)
	return nil
}

// Close is a no-op.
func (c *ConsoleStore) Close() error { return nil }

// FlowMessage emits an ad-hoc flow-level pterm line.
func (c *ConsoleStore) FlowMessage(flow string, isError bool, msg string) {
	emitFlowPterm(flow, isError, msg)
}

var _ RunStore = (*ConsoleStore)(nil)
var _ FlowMessenger = (*ConsoleStore)(nil)

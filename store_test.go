package goblin_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ruivieira/goblin"
)

type memStore struct {
	begins, ends       []goblin.RunRecord
	taskBegins, taskEnds []goblin.TaskRecord
	closed             int
	failBegin          error
}

func (m *memStore) BeginRun(r goblin.RunRecord) error {
	m.begins = append(m.begins, r)
	return m.failBegin
}
func (m *memStore) EndRun(r goblin.RunRecord) error {
	m.ends = append(m.ends, r)
	return nil
}
func (m *memStore) BeginTask(t goblin.TaskRecord) error {
	m.taskBegins = append(m.taskBegins, t)
	return nil
}
func (m *memStore) EndTask(t goblin.TaskRecord) error {
	m.taskEnds = append(m.taskEnds, t)
	return nil
}
func (m *memStore) Close() error {
	m.closed++
	return nil
}

func TestMultiStoreFansOut(t *testing.T) {
	a, b := &memStore{}, &memStore{}
	ms := goblin.MultiStore(a, b, nil)

	rec := goblin.RunRecord{ID: "r1", Flow: "f", Status: goblin.RunStatusRunning, StartedAt: time.Now().UTC()}
	if err := ms.BeginRun(rec); err != nil {
		t.Fatal(err)
	}
	task := goblin.TaskRecord{RunID: "r1", Task: "t", Status: goblin.RunStatusRunning, StartedAt: time.Now().UTC()}
	if err := ms.BeginTask(task); err != nil {
		t.Fatal(err)
	}
	task.Status = goblin.RunStatusCompleted
	task.FinishedAt = time.Now().UTC()
	if err := ms.EndTask(task); err != nil {
		t.Fatal(err)
	}
	rec.Status = goblin.RunStatusCompleted
	rec.FinishedAt = time.Now().UTC()
	if err := ms.EndRun(rec); err != nil {
		t.Fatal(err)
	}
	if err := ms.Close(); err != nil {
		t.Fatal(err)
	}

	for _, m := range []*memStore{a, b} {
		if len(m.begins) != 1 || len(m.ends) != 1 || len(m.taskBegins) != 1 || len(m.taskEnds) != 1 || m.closed != 1 {
			t.Fatalf("unexpected counts: begins=%d ends=%d taskBegins=%d taskEnds=%d closed=%d",
				len(m.begins), len(m.ends), len(m.taskBegins), len(m.taskEnds), m.closed)
		}
	}
}

func TestMultiStoreJoinsErrors(t *testing.T) {
	a := &memStore{failBegin: errors.New("a-fail")}
	b := &memStore{}
	ms := goblin.MultiStore(a, b)
	err := ms.BeginRun(goblin.RunRecord{ID: "r"})
	if err == nil || !strings.Contains(err.Error(), "a-fail") {
		t.Fatalf("got %v, want joined a-fail", err)
	}
	if len(b.begins) != 1 {
		t.Fatalf("expected b still called, got %d", len(b.begins))
	}
}

func TestConsoleStoreEndRunAndTask(t *testing.T) {
	var buf bytes.Buffer
	goblin.SetOutput(&buf)
	t.Cleanup(func() { goblin.SetOutput(nil) })

	cs := goblin.NewConsoleStore()
	if err := cs.BeginRun(goblin.RunRecord{ID: "1", Flow: "demo"}); err != nil {
		t.Fatal(err)
	}
	if err := cs.EndTask(goblin.TaskRecord{
		Task:     "greet",
		Status:   goblin.RunStatusCompleted,
		Messages: []string{"Finished in state Completed()"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := cs.EndRun(goblin.RunRecord{Flow: "demo", Status: goblin.RunStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Task run 'greet'") || !strings.Contains(out, "Flow run 'demo'") {
		t.Fatalf("unexpected console output: %q", out)
	}
}

func TestSyslogStoreRFC3164(t *testing.T) {
	var buf bytes.Buffer
	s := goblin.NewSyslogStore(&buf)
	now := time.Date(2026, 7, 24, 19, 1, 0, 0, time.Local)
	if err := s.BeginRun(goblin.RunRecord{
		ID: "abc", Flow: "install-evalhub", Status: goblin.RunStatusRunning, StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.EndTask(goblin.TaskRecord{
		RunID: "abc", Task: "git-clone", Status: goblin.RunStatusCompleted,
		Messages: []string{"done"}, FinishedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.EndRun(goblin.RunRecord{
		ID: "abc", Flow: "install-evalhub", Status: goblin.RunStatusCompleted, FinishedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("want >=3 lines, got %q", buf.String())
	}
	// RFC 3164: "Mmm dd hh:mm:ss hostname tag: message"
	re := regexp.MustCompile(`^[A-Z][a-z]{2}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}\s+\S+\s+goblin:\s+`)
	for i, line := range lines {
		if !re.MatchString(line) {
			t.Fatalf("line %d not RFC 3164: %q", i, line)
		}
	}
	joined := buf.String()
	if !strings.Contains(joined, "flow=install-evalhub") || !strings.Contains(joined, "run=abc") {
		t.Fatalf("missing flow/run fields: %q", joined)
	}
	if !strings.Contains(joined, "task=git-clone") || !strings.Contains(joined, "status=completed") {
		t.Fatalf("missing task fields: %q", joined)
	}
	if !strings.Contains(joined, `message=done`) {
		t.Fatalf("missing message: %q", joined)
	}
}

func TestSyslogFileStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goblin.log")
	s, err := goblin.NewSyslogFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BeginRun(goblin.RunRecord{ID: "x", Flow: "f", Status: goblin.RunStatusRunning, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.db")
	s, err := goblin.NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	start := time.Now().UTC().Truncate(time.Millisecond)
	run := goblin.RunRecord{ID: "run-1", Flow: "demo", Status: goblin.RunStatusRunning, StartedAt: start}
	if err := s.BeginRun(run); err != nil {
		t.Fatal(err)
	}
	task := goblin.TaskRecord{RunID: "run-1", Task: "step-a", Status: goblin.RunStatusRunning, StartedAt: start}
	if err := s.BeginTask(task); err != nil {
		t.Fatal(err)
	}
	task.Status = goblin.RunStatusCompleted
	task.Messages = []string{"Finished in state Completed()"}
	task.FinishedAt = start.Add(time.Second)
	if err := s.EndTask(task); err != nil {
		t.Fatal(err)
	}
	run.Status = goblin.RunStatusCompleted
	run.FinishedAt = start.Add(2 * time.Second)
	if err := s.EndRun(run); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetRun("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Flow != "demo" || got.Status != goblin.RunStatusCompleted {
		t.Fatalf("got %+v", got)
	}
	tasks, err := s.ListTasks("run-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Task != "step-a" || tasks[0].Status != goblin.RunStatusCompleted {
		t.Fatalf("tasks=%+v", tasks)
	}
	if len(tasks[0].Messages) != 1 || !strings.Contains(tasks[0].Messages[0], "Completed()") {
		t.Fatalf("messages=%v", tasks[0].Messages)
	}
}

type storeGreetConfig struct{ Name string }
type storeGreeted struct{ Msg string }

func TestRunWithMultiStoreIntegration(t *testing.T) {
	var consoleBuf bytes.Buffer
	goblin.SetOutput(&consoleBuf)
	t.Cleanup(func() { goblin.SetOutput(nil) })

	var syslogBuf bytes.Buffer
	dbPath := filepath.Join(t.TempDir(), "integ.db")
	sqlite, err := goblin.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })

	console := goblin.NewConsoleStore()
	syslog := goblin.NewSyslogStore(&syslogBuf)
	logger := &goblin.Logger{
		Flow:   "integ-flow",
		Stores: []goblin.RunStore{goblin.MultiStore(console, sqlite, syslog)},
	}
	deps := goblin.Deps{Logger: logger}

	greet := goblin.New(goblin.Spec{
		Name: "store-greet",
		Fn: func(_ context.Context, d goblin.Deps, cfg storeGreetConfig) (storeGreeted, error) {
			d.Logger.TaskInfo("store-greet", "hello "+cfg.Name)
			return storeGreeted{Msg: "hello " + cfg.Name}, nil
		},
	})

	err = goblin.Run(context.Background(), logger,
		[]any{deps, storeGreetConfig{Name: "goblin"}},
		[]any{greet},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(consoleBuf.String(), "Task run 'store-greet'") {
		t.Fatalf("console missing task: %q", consoleBuf.String())
	}
	if !strings.Contains(consoleBuf.String(), "Flow run 'integ-flow'") {
		t.Fatalf("console missing flow: %q", consoleBuf.String())
	}
	if !strings.Contains(syslogBuf.String(), "status=completed") || !strings.Contains(syslogBuf.String(), "task=store-greet") {
		t.Fatalf("syslog missing fields: %q", syslogBuf.String())
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runs WHERE flow = ? AND status = ?`, "integ-flow", "completed").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 completed run, got %d", n)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE task = ? AND status = ?`, "store-greet", "completed").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 completed task, got %d", n)
	}
}

func TestDefaultStoreIsConsole(t *testing.T) {
	var buf bytes.Buffer
	goblin.SetOutput(&buf)
	t.Cleanup(func() { goblin.SetOutput(nil) })

	logger := &goblin.Logger{Flow: "default-console"}
	logger.FlowInfo("hello")
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("expected default console output, got %q", buf.String())
	}
}

package goblin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore persists run and task records in a SQLite database.
type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex
}

// NewSQLiteStore opens (or creates) a SQLite database at path and ensures schema.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("goblin: mkdir for sqlite: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("goblin: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS runs (
	id TEXT PRIMARY KEY,
	flow TEXT NOT NULL,
	status TEXT NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	started_at TEXT NOT NULL,
	finished_at TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	run_id TEXT NOT NULL,
	task TEXT NOT NULL,
	status TEXT NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	messages_json TEXT NOT NULL DEFAULT '[]',
	started_at TEXT NOT NULL,
	finished_at TEXT NOT NULL DEFAULT '',
	FOREIGN KEY(run_id) REFERENCES runs(id)
);
CREATE INDEX IF NOT EXISTS idx_tasks_run_id ON tasks(run_id);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("goblin: sqlite migrate: %w", err)
	}
	return nil
}

// BeginRun inserts a running flow record.
func (s *SQLiteStore) BeginRun(r RunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO runs (id, flow, status, error, started_at, finished_at) VALUES (?, ?, ?, ?, ?, ?)`,
		r.ID, r.Flow, string(r.Status), r.Error, formatTime(r.StartedAt), "",
	)
	if err != nil {
		return fmt.Errorf("goblin: sqlite BeginRun: %w", err)
	}
	return nil
}

// EndRun updates the flow record with final status.
func (s *SQLiteStore) EndRun(r RunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`UPDATE runs SET status = ?, error = ?, finished_at = ? WHERE id = ?`,
		string(r.Status), r.Error, formatTime(r.FinishedAt), r.ID,
	)
	if err != nil {
		return fmt.Errorf("goblin: sqlite EndRun: %w", err)
	}
	return nil
}

// BeginTask inserts a running task row.
func (s *SQLiteStore) BeginTask(t TaskRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO tasks (run_id, task, status, error, messages_json, started_at, finished_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.RunID, t.Task, string(t.Status), t.Error, "[]", formatTime(t.StartedAt), "",
	)
	if err != nil {
		return fmt.Errorf("goblin: sqlite BeginTask: %w", err)
	}
	return nil
}

// EndTask updates the latest matching task row with final status and messages.
func (s *SQLiteStore) EndTask(t TaskRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	msgs, err := json.Marshal(t.Messages)
	if err != nil {
		return fmt.Errorf("goblin: sqlite marshal messages: %w", err)
	}
	if t.Messages == nil {
		msgs = []byte("[]")
	}
	res, err := s.db.Exec(
		`UPDATE tasks SET status = ?, error = ?, messages_json = ?, finished_at = ?
		 WHERE id = (
		   SELECT id FROM tasks WHERE run_id = ? AND task = ? AND status = ? ORDER BY id DESC LIMIT 1
		 )`,
		string(t.Status), t.Error, string(msgs), formatTime(t.FinishedAt),
		t.RunID, t.Task, string(RunStatusRunning),
	)
	if err != nil {
		return fmt.Errorf("goblin: sqlite EndTask: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Task began without BeginTask (should not happen); insert finished row.
		_, err = s.db.Exec(
			`INSERT INTO tasks (run_id, task, status, error, messages_json, started_at, finished_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			t.RunID, t.Task, string(t.Status), t.Error, string(msgs), formatTime(t.StartedAt), formatTime(t.FinishedAt),
		)
		if err != nil {
			return fmt.Errorf("goblin: sqlite EndTask insert: %w", err)
		}
	}
	return nil
}

// Close closes the database.
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// GetRun returns a run by ID (test/helper).
func (s *SQLiteStore) GetRun(id string) (RunRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var r RunRecord
	var started, finished string
	err := s.db.QueryRow(
		`SELECT id, flow, status, error, started_at, finished_at FROM runs WHERE id = ?`, id,
	).Scan(&r.ID, &r.Flow, &r.Status, &r.Error, &started, &finished)
	if err != nil {
		return r, err
	}
	r.StartedAt, _ = parseTime(started)
	r.FinishedAt, _ = parseTime(finished)
	return r, nil
}

// ListTasks returns tasks for a run (test/helper).
func (s *SQLiteStore) ListTasks(runID string) ([]TaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(
		`SELECT run_id, task, status, error, messages_json, started_at, finished_at FROM tasks WHERE run_id = ? ORDER BY id`,
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskRecord
	for rows.Next() {
		var t TaskRecord
		var msgs, started, finished string
		if err := rows.Scan(&t.RunID, &t.Task, &t.Status, &t.Error, &msgs, &started, &finished); err != nil {
			return nil, err
		}
		if msgs != "" && msgs != "[]" {
			_ = json.Unmarshal([]byte(msgs), &t.Messages)
		}
		t.StartedAt, _ = parseTime(started)
		t.FinishedAt, _ = parseTime(finished)
		out = append(out, t)
	}
	return out, rows.Err()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

var _ RunStore = (*SQLiteStore)(nil)

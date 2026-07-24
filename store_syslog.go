package goblin

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// SyslogStore writes BSD syslog RFC 3164 lines to an io.Writer.
// Example:
//
//	Jul 24 19:01:00 hostname goblin: flow=install-evalhub run=abc task=git-clone status=completed
type SyslogStore struct {
	w        io.Writer
	hostname string
	tag      string
	mu       sync.Mutex
}

// NewSyslogStore writes RFC 3164 lines to w. Hostname defaults to the local host;
// the syslog TAG is "goblin".
func NewSyslogStore(w io.Writer) *SyslogStore {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}
	return &SyslogStore{w: w, hostname: host, tag: "goblin"}
}

// NewSyslogFileStore appends RFC 3164 lines to path, creating the file if needed.
func NewSyslogFileStore(path string) (*SyslogStore, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("goblin: open syslog file: %w", err)
	}
	s := NewSyslogStore(f)
	return s, nil
}

// BeginRun writes a running flow line.
func (s *SyslogStore) BeginRun(r RunRecord) error {
	return s.write(r.StartedAt, fmt.Sprintf("flow=%s run=%s status=%s", quote(r.Flow), r.ID, r.Status))
}

// EndRun writes the final flow status.
func (s *SyslogStore) EndRun(r RunRecord) error {
	msg := fmt.Sprintf("flow=%s run=%s status=%s", quote(r.Flow), r.ID, r.Status)
	if r.Error != "" {
		msg += " error=" + quote(r.Error)
	}
	ts := r.FinishedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	return s.write(ts, msg)
}

// BeginTask writes a running task line.
func (s *SyslogStore) BeginTask(t TaskRecord) error {
	return s.write(t.StartedAt, fmt.Sprintf("flow_run=%s task=%s status=%s", t.RunID, quote(t.Task), t.Status))
}

// EndTask writes the final task status and messages.
func (s *SyslogStore) EndTask(t TaskRecord) error {
	msg := fmt.Sprintf("flow_run=%s task=%s status=%s", t.RunID, quote(t.Task), t.Status)
	if t.Error != "" {
		msg += " error=" + quote(t.Error)
	}
	for _, m := range t.Messages {
		msg += " message=" + quote(m)
	}
	ts := t.FinishedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	return s.write(ts, msg)
}

// Close closes the underlying writer if it implements io.Closer.
func (s *SyslogStore) Close() error {
	if c, ok := s.w.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (s *SyslogStore) write(ts time.Time, msg string) error {
	if ts.IsZero() {
		ts = time.Now()
	}
	// RFC 3164 timestamp: "Mmm dd hh:mm:ss" (day is space-padded).
	stamp := ts.Local().Format("Jan _2 15:04:05")
	line := fmt.Sprintf("%s %s %s: %s\n", stamp, s.hostname, s.tag, msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := io.WriteString(s.w, line)
	return err
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	if strings.IndexFunc(s, func(r rune) bool {
		return r == ' ' || r == '=' || r == '"'
	}) >= 0 {
		return `"` + strings.ReplaceAll(s, `"`, `'`) + `"`
	}
	return s
}

var _ RunStore = (*SyslogStore)(nil)

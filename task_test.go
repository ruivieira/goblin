package goblin_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ruivieira/goblin"
)

func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = old
	})

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String()
}

func TestDoSuccess(t *testing.T) {
	logger := &goblin.Logger{Flow: "test-flow"}
	out := captureOutput(t, func() {
		if err := goblin.Do(logger, "my-task", func() error { return nil }); err != nil {
			t.Fatalf("Do: %v", err)
		}
	})
	if !strings.Contains(out, "Task run 'my-task'") {
		t.Fatalf("expected task log, got: %q", out)
	}
	if !strings.Contains(out, "message:") || !strings.Contains(out, "Completed()") {
		t.Fatalf("expected completed submessage, got: %q", out)
	}
}

func TestDoFailure(t *testing.T) {
	logger := &goblin.Logger{Flow: "test-flow"}
	want := errors.New("boom")
	out := captureOutput(t, func() {
		err := goblin.Do(logger, "fail-task", func() error { return want })
		if !errors.Is(err, want) {
			t.Fatalf("got %v, want %v", err, want)
		}
	})
	if !strings.Contains(out, "Failed('boom')") {
		t.Fatalf("expected failure log, got: %q", out)
	}
}

func TestDoValue(t *testing.T) {
	logger := &goblin.Logger{Flow: "test-flow"}
	v, err := goblin.DoValue(logger, "value-task", func() (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if v != "ok" {
		t.Fatalf("got %q, want ok", v)
	}
}

package goblin_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ruivieira/goblin"
)

type seedA struct{ N int }
type outA struct{ V string }
type outB struct{ V string }

func TestNewPanicsOnBadFn(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	_ = goblin.New(goblin.Spec{Name: "bad", Fn: "not-a-func"})
}

func TestRegistry(t *testing.T) {
	// Use unique names; registry is process-global.
	a := goblin.New(goblin.Spec{
		Name:        "test-registry-alpha",
		Description: "alpha",
		Fn: func(ctx context.Context, deps goblin.Deps, s seedA) (outA, error) {
			return outA{V: "a"}, nil
		},
	})
	goblin.Register(a)
	if got := goblin.ByName("test-registry-alpha"); got == nil || got.Name() != "test-registry-alpha" {
		t.Fatalf("ByName: got %#v", got)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate panic")
		}
	}()
	goblin.Register(a)
}

func TestRunActionAutoLog(t *testing.T) {
	logger := &goblin.Logger{Flow: "test-action-log"}
	deps := goblin.Deps{Logger: logger}
	action := goblin.New(goblin.Spec{
		Name:        "produce-a",
		Description: "produces outA",
		Fn: func(ctx context.Context, deps goblin.Deps, s seedA) (outA, error) {
			deps.Logger.TaskInfo("produce-a", "working")
			return outA{V: "ok"}, nil
		},
	})

	out := captureOutput(t, func() {
		err := goblin.Run(context.Background(), logger,
			[]any{deps, seedA{N: 1}},
			[]any{action},
			nil,
		)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
	if !strings.Contains(out, "Task run 'produce-a'") {
		t.Fatalf("expected auto task log, got: %q", out)
	}
	if !strings.Contains(out, "Completed()") {
		t.Fatalf("expected Completed, got: %q", out)
	}
}

func TestRunValidateFails(t *testing.T) {
	logger := &goblin.Logger{Flow: "test-validate"}
	deps := goblin.Deps{Logger: logger}
	want := errors.New("bad seed")
	action := goblin.New(goblin.Spec{
		Name: "validate-step",
		Fn: func(ctx context.Context, deps goblin.Deps, s seedA) (outA, error) {
			return outA{V: "should-not-run"}, nil
		},
		Validate: func(ctx context.Context, deps goblin.Deps, s seedA) error {
			return want
		},
	})

	err := goblin.Run(context.Background(), logger,
		[]any{deps, seedA{}},
		[]any{action},
		nil,
	)
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

func TestRunCompensateOnDownstreamFailure(t *testing.T) {
	logger := &goblin.Logger{Flow: "test-compensate"}
	deps := goblin.Deps{Logger: logger}
	var compensated bool

	stepA := goblin.New(goblin.Spec{
		Name: "step-a-ok",
		Fn: func(ctx context.Context, deps goblin.Deps, s seedA) (outA, error) {
			return outA{V: "a"}, nil
		},
		Compensate: func(ctx context.Context, r outA) error {
			if r.V != "a" {
				t.Errorf("compensate got %+v", r)
			}
			compensated = true
			return nil
		},
	})
	stepB := goblin.New(goblin.Spec{
		Name: "step-b-fail",
		Fn: func(ctx context.Context, deps goblin.Deps, a outA) (outB, error) {
			return outB{}, errors.New("boom")
		},
	})

	err := goblin.Run(context.Background(), logger,
		[]any{deps, seedA{N: 1}},
		[]any{stepA, stepB},
		nil,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !compensated {
		t.Fatal("expected compensate to run")
	}
}

func TestRunRawFuncCompat(t *testing.T) {
	logger := &goblin.Logger{Flow: "test-raw"}
	deps := goblin.Deps{Logger: logger}
	raw := func(ctx context.Context, deps goblin.Deps, s seedA) (outA, error) {
		return outA{V: "raw"}, nil
	}
	var got outA
	err := goblin.Run(context.Background(), logger,
		[]any{deps, seedA{}},
		[]any{raw},
		func(results map[any]any) {
			if v, ok := results[outA{}]; ok {
				got = v.(outA)
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.V != "raw" {
		t.Fatalf("got %+v", got)
	}
}

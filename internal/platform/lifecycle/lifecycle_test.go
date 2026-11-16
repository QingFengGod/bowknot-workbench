package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCloseRunsHooksInReverseOrder(t *testing.T) {
	lc := New()
	var order []string

	lc.Add("first", func(context.Context) error {
		order = append(order, "first")
		return nil
	})
	lc.Add("second", func(context.Context) error {
		order = append(order, "second")
		return nil
	})

	if err := lc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	want := []string{"second", "first"}
	if !equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestCloseKeepsGoingAfterError(t *testing.T) {
	lc := New()
	var ran bool

	lc.Add("failing", func(context.Context) error {
		return errors.New("boom")
	})
	lc.Add("after", func(context.Context) error {
		ran = true
		return nil
	})

	err := lc.Close()
	if err == nil || !strings.Contains(err.Error(), "failing") {
		t.Fatalf("Close() error = %v, want wrapped failing error", err)
	}
	if !ran {
		t.Fatal("later hook did not run after earlier error")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	lc := New()
	runs := 0
	lc.Add("once", func(context.Context) error {
		runs++
		return nil
	})

	if err := lc.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := lc.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if runs != 1 {
		t.Fatalf("hook ran %d times, want 1", runs)
	}
}

func TestCloseCancelsContext(t *testing.T) {
	lc := New()
	ctx := lc.Context()
	if err := ctx.Err(); err != nil {
		t.Fatalf("context already done: %v", err)
	}

	if err := lc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context error = %v, want context.Canceled", ctx.Err())
	}
}

func TestAddAfterCloseRunsImmediately(t *testing.T) {
	lc := New()
	if err := lc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	ran := false
	lc.Add("late", func(context.Context) error {
		ran = true
		return nil
	})
	if !ran {
		t.Fatal("late hook did not run")
	}
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

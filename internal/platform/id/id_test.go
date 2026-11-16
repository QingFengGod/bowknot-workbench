package id

import (
	"strings"
	"testing"
	"time"
)

func TestNewHasFixedShape(t *testing.T) {
	got := New()
	if len(got) != 26 {
		t.Fatalf("len(New()) = %d, want 26: %q", len(got), got)
	}
	if got != strings.ToLower(got) {
		t.Fatalf("New() = %q, want lowercase", got)
	}
	for _, r := range got[:13] {
		if r < '0' || r > '9' {
			t.Fatalf("New() = %q, want 13-digit timestamp prefix", got)
		}
	}
}

func TestNewIsUnique(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		got := New()
		if _, ok := seen[got]; ok {
			t.Fatalf("duplicate id: %s", got)
		}
		seen[got] = struct{}{}
	}
}

func TestNewIsTimeSortable(t *testing.T) {
	first := New()
	time.Sleep(5 * time.Millisecond)
	second := New()

	if first >= second {
		t.Fatalf("ids not sorted by time: %q >= %q", first, second)
	}
}

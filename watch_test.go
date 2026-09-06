package kgraph

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func scratchProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"+FactsSuffix),
		[]byte("```kfacts\n- id: a\n  claim: A\n  status: asserted\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// fastWatch shortens the poll so a test does not wait a real second.
func fastWatch(t *testing.T, r *Registry, dir string) (<-chan Change, func()) {
	t.Helper()
	ch, cancel, err := r.Watch(dir)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	for _, w := range r.watchers {
		w.mu.Lock()
		w.interval = 20 * time.Millisecond
		w.mu.Unlock()
	}
	r.mu.Unlock()
	return ch, cancel
}

func TestWatchNotifiesOnChange(t *testing.T) {
	dir := scratchProject(t)
	r := NewRegistry()
	ch, cancel := fastWatch(t, r, dir)
	defer cancel()

	// Nothing changed: no event. A watcher that fires spuriously would make the
	// console reload constantly and teach the user to ignore it.
	select {
	case c := <-ch:
		t.Fatalf("unexpected change: %+v", c)
	case <-time.After(200 * time.Millisecond):
	}

	if err := os.WriteFile(filepath.Join(dir, "b"+FactsSuffix),
		[]byte("```kfacts\n- id: b\n  claim: B\n  status: asserted\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-ch:
		if c.Root == "" || c.Fingerprint == "" || c.At == "" {
			t.Fatalf("incomplete change: %+v", c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no change delivered")
	}
}

// Ten browser tabs must cost one stat sweep, not ten.
func TestWatchSharesOnePollerAcrossSubscribers(t *testing.T) {
	dir := scratchProject(t)
	r := NewRegistry()
	a, ca := fastWatch(t, r, dir)
	b, cb := fastWatch(t, r, dir)
	defer ca()
	defer cb()

	r.mu.Lock()
	n := len(r.watchers)
	r.mu.Unlock()
	if n != 1 {
		t.Fatalf("want one watcher for two subscribers, got %d", n)
	}

	if err := os.WriteFile(filepath.Join(dir, "c"+FactsSuffix),
		[]byte("```kfacts\n- id: c\n  claim: C\n  status: asserted\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, ch := range []<-chan Change{a, b} {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatalf("subscriber %d received nothing", i)
		}
	}
}

// The poller must stop when the last subscriber leaves, or a long-lived daemon
// accumulates a stat sweep per project ever opened.
func TestWatchStopsWithTheLastSubscriber(t *testing.T) {
	dir := scratchProject(t)
	r := NewRegistry()
	_, ca := fastWatch(t, r, dir)
	_, cb := fastWatch(t, r, dir)

	ca()
	r.mu.Lock()
	still := len(r.watchers)
	r.mu.Unlock()
	if still != 1 {
		t.Fatalf("one subscriber remains; the poller must survive: %d", still)
	}

	cb()
	r.mu.Lock()
	gone := len(r.watchers)
	r.mu.Unlock()
	if gone != 0 {
		t.Fatalf("the last unsubscribe must stop the poller: %d watchers", gone)
	}
}

// A slow reader must not stall the poller or its siblings.
func TestWatchDropsRatherThanBlocks(t *testing.T) {
	dir := scratchProject(t)
	r := NewRegistry()
	slow, cancel := fastWatch(t, r, dir)
	defer cancel()

	for i := range 12 {
		p := filepath.Join(dir, "f"+string(rune('a'+i))+FactsSuffix)
		if err := os.WriteFile(p, []byte("```kfacts\n- id: z\n  claim: Z\n```\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Millisecond)
	}
	// The buffer is small on purpose; what matters is that the poller kept going
	// and the channel still yields the latest state.
	select {
	case <-slow:
	case <-time.After(2 * time.Second):
		t.Fatal("the poller stalled behind a slow reader")
	}
}

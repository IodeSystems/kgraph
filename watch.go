package kgraph

import (
	"sync"
	"time"
)

// Change is one notification that a scope's files moved.
type Change struct {
	Root        string `json:"root"`
	Branch      string `json:"branch"`
	Fingerprint string `json:"fingerprint"`
	At          string `json:"at"`
}

// Watching is polled, not inotify. The fingerprint is already a stat sweep
// designed to be cheap, an editor save is not a latency-critical event, and
// polling has no descriptor limits, no missed-event races on rename, and works
// the same on every filesystem including the network mounts `~/life` syncs over.
const watchInterval = time.Second

// watcher polls one scope and fans out to every subscriber, so ten browser tabs
// cost one stat sweep rather than ten.
type watcher struct {
	root, branch string
	// index is the closed graph this watcher polls for. Two indexes in one
	// checkout are two watchers.
	index    string
	interval time.Duration
	// reg supplies the evidence paths to stat. The watcher cannot derive them —
	// only a loaded graph knows which documents are declared — so it asks the
	// registry for whatever the last load found.
	reg *Registry

	mu   sync.Mutex
	subs map[int]chan Change
	next int
	last string
	stop chan struct{}
}

// Watch subscribes to changes for the scope containing dir. The returned cancel
// must be called; the poller stops when the last subscriber leaves.
func (r *Registry) Watch(dir string) (<-chan Change, func(), error) {
	root, err := DiscoverRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	branch := Branch(root)
	// One watcher per INDEX, not per checkout — a watcher keyed without it would
	// report one index's changes to a subscriber watching another.
	index, err := IndexAt(root, dir)
	if err != nil {
		return nil, nil, err
	}
	key := scopeKey(root, branch, index)

	r.mu.Lock()
	if r.watchers == nil {
		r.watchers = map[string]*watcher{}
	}
	w := r.watchers[key]
	if w == nil {
		fp, _ := fingerprint(root, r.docsLocked(root, branch, index))
		w = &watcher{root: root, branch: branch, index: index, interval: watchInterval, reg: r,
			subs: map[int]chan Change{}, last: fp, stop: make(chan struct{})}
		r.watchers[key] = w
		go w.run()
	}
	r.mu.Unlock()

	ch, id := w.subscribe()
	return ch, func() {
		if w.unsubscribe(id) {
			r.mu.Lock()
			if r.watchers[key] == w {
				delete(r.watchers, key)
			}
			r.mu.Unlock()
		}
	}, nil
}

// Docs returns the evidence paths the last load of a scope declared.
func (r *Registry) Docs(root, branch, index string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.docsLocked(root, branch, index)
}

func (r *Registry) docsLocked(root, branch, index string) []string {
	if sc, ok := r.scopes[scopeKey(root, branch, index)]; ok {
		return sc.docs
	}
	return nil
}

func (w *watcher) subscribe() (chan Change, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.next++
	// Buffered: a slow reader must never stall the poller or its siblings.
	ch := make(chan Change, 4)
	w.subs[w.next] = ch
	return ch, w.next
}

// unsubscribe reports whether that was the last subscriber, in which case the
// poller has been stopped.
func (w *watcher) unsubscribe(id int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ch, ok := w.subs[id]; ok {
		delete(w.subs, id)
		close(ch)
	}
	if len(w.subs) == 0 {
		select {
		case <-w.stop:
		default:
			close(w.stop)
		}
		return true
	}
	return false
}

func (w *watcher) run() {
	// A timer per iteration, with the interval re-read under the lock each time.
	// One long-lived Ticker built from w.interval before the loop both races with
	// anything adjusting the interval and ignores it — the ticker already exists —
	// so an adjustment raced AND silently did nothing.
	for {
		w.mu.Lock()
		iv := w.interval
		w.mu.Unlock()
		t := time.NewTimer(iv)
		select {
		case <-w.stop:
			t.Stop()
			return
		case <-t.C:
			fp, err := fingerprint(w.root, w.reg.Docs(w.root, w.branch, w.index))
			if err != nil || fp == "" {
				continue
			}
			w.mu.Lock()
			changed := fp != w.last
			if changed {
				w.last = fp
			}
			subs := make([]chan Change, 0, len(w.subs))
			for _, ch := range w.subs {
				subs = append(subs, ch)
			}
			w.mu.Unlock()
			if !changed {
				continue
			}
			c := Change{Root: w.root, Branch: w.branch, Fingerprint: fp,
				At: time.Now().UTC().Format(time.RFC3339)}
			for _, ch := range subs {
				// Drop rather than block: the message is "something changed", and a
				// client that missed one will learn it from the next.
				select {
				case ch <- c:
				default:
				}
			}
		}
	}
}

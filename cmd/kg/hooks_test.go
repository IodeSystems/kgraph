package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The hooks are the only place kgraph turns detection into prevention, so the
// blocking ones are pinned in both directions: they must refuse the thing that
// corrupts state, and must not refuse ordinary work.

func ev(t *testing.T, cwd, path string, kv map[string]any) hookEvent {
	t.Helper()
	in := map[string]any{"file_path": path}
	for k, v := range kv {
		in[k] = v
	}
	b, err := json.Marshal(map[string]any{"cwd": cwd, "tool_name": "Edit", "tool_input": in})
	if err != nil {
		t.Fatal(err)
	}
	var out hookEvent
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHookEventNormalisesEdits(t *testing.T) {
	single := ev(t, "/x", "a.md", map[string]any{"old_string": "o", "new_string": "n"})
	if got := single.edits(); len(got) != 1 || got[0].old != "o" || got[0].new != "n" {
		t.Fatalf("Edit: %+v", got)
	}
	multi := ev(t, "/x", "a.md", map[string]any{"edits": []any{
		map[string]any{"old_string": "a", "new_string": "b"},
		map[string]any{"old_string": "c", "new_string": "d"},
	}})
	if got := multi.edits(); len(got) != 2 || got[1].old != "c" {
		t.Fatalf("MultiEdit: %+v", got)
	}
	write := ev(t, "/x", "a.md", map[string]any{"content": "whole file"})
	if got := write.edits(); len(got) != 0 {
		t.Fatalf("Write has no edits, got %+v", got)
	}
}

// TestPendingContentReconstructsTheWrite retired 2026-08-30 with `what-if` and
// `lint-facts`. Both fired on an EDIT to a `*.kfacts.md`, and facts are not
// edited as files any more. What they protected moved INSIDE the write path,
// where `Apply` refuses anything that would not load — a guard that cannot be
// bypassed by declining to install a hook.

// hookPresent is what makes `kg hook install` idempotent.
func TestHookPresentDetectsAnExistingCommand(t *testing.T) {
	entries := []any{map[string]any{
		"matcher": "Edit",
		"hooks":   []any{map[string]any{"type": "command", "command": "kg hook lint-facts"}},
	}}
	if !hookPresent(entries, "kg hook lint-facts") {
		t.Fatal("should find the installed command")
	}
	if hookPresent(entries, "kg hook what-if") {
		t.Fatal("should not match a different command")
	}
	if hookPresent(nil, "kg hook lint-facts") {
		t.Fatal("nil entries match nothing")
	}
}

func TestHookInstallIsIdempotentAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	o := opts{root: dir}

	if err := hookInstall(o, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude", "settings.json")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(first, &settings); err != nil {
		t.Fatal(err)
	}
	// No PreToolUse hooks left: both were triggers on a fact-file edit, and the
	// checks they ran now live inside the write path.
	if len(settings.Hooks["PreToolUse"]) != 0 {
		t.Fatalf("want no PreToolUse hooks, got %d", len(settings.Hooks["PreToolUse"]))
	}
	if len(settings.Hooks["Stop"]) != 1 {
		t.Fatalf("want 1 Stop hook, got %d", len(settings.Hooks["Stop"]))
	}
	// Stop takes no matcher; a matcher there would never fire.
	if m := settings.Hooks["Stop"][0].Matcher; m != "" {
		t.Fatalf("Stop must carry no matcher, got %q", m)
	}

	if err := hookInstall(o, nil); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(first) {
		t.Fatal("a second install must not change the file")
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatal("an existing settings file must be backed up before rewriting")
	}
}

// Foreign settings must survive: this merges into a shared file.
func TestHookInstallPreservesExistingSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude", "settings.json")
	existing := `{"model":"opus","hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"other-tool check"}]}]}}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := hookInstall(opts{root: dir}, nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"model": "opus"`, "other-tool check", "hook report-stale"} {
		if !strings.Contains(got, want) {
			t.Errorf("merged settings must keep %q:\n%s", want, got)
		}
	}
}

func TestHookInstallRefusesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude", "settings.json")
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := hookInstall(opts{root: dir}, nil)
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("must refuse rather than clobber, got %v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "{ not json" {
		t.Fatal("the file must be left untouched")
	}
}

// Every installed command must be one the binary actually dispatches — a typo
// here produces a hook that silently does nothing on every tool call.
func TestInstallableNamesAreAllDispatched(t *testing.T) {
	dispatched := map[string]bool{"report-stale": true}
	for _, h := range installable {
		if !dispatched[h.name] {
			t.Errorf("installable hook %q is not dispatched by cmdHook", h.name)
		}
		if !strings.Contains(hookUsage, h.name) {
			t.Errorf("installable hook %q is undocumented in hookUsage", h.name)
		}
	}
	if len(installable) != len(dispatched) {
		t.Errorf("installable has %d entries, %d are dispatched", len(installable), len(dispatched))
	}
}

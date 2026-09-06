package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iodesystems/kgraph"
)

// Claude Code hooks. Each reads the event JSON on stdin and answers with an exit
// code: 0 lets the tool proceed (stdout becomes context for the model), 2 blocks
// it and feeds stderr back so the model can correct.
//
// Two rules run through all of these:
//
//   - NOTHING BLOCKS ANY MORE, and that is a better place to be than it sounds.
//     `guard-managed` went with generation. `lint-facts` and `what-if` both
//     triggered on an EDIT to a `*.kfacts.md` — reporting scan errors after one,
//     and a pending change's blast radius before one — and facts are not edited
//     as files any longer. Their protection did not disappear; it moved INSIDE
//     the write path, where `Apply` folds the log a write would join, builds the
//     result, and refuses anything that would not load. That cannot be bypassed
//     by declining to install a hook, which is strictly better than a guard that
//     is opt-in.
//     `guard-managed` used to be the other one and was the load-bearing hook:
//     a hand-edited managed block made a stale document report itself FRESH,
//     which nothing downstream could detect. It went with generation — there is
//     no managed block to guard, and a hook refusing edits to a marker that no
//     longer appears in any file is a hook that only ever fires wrongly.
//     A hook that blocks on something the user legitimately does — hand-editing a
//     generated document, say — trains them to remove the hook, which costs more
//     than the hook saves.
//   - An internal failure fails OPEN. A broken hook must never wedge a session,
//     so a parse error or a missing repo exits 0 silently. The one exception is
//     the check whose entire job is to refuse.

const hookUsage = `kg hook <name> — Claude Code hooks (event JSON on stdin)

  report-stale    Stop         which standing answers moved this session
  install         —            merge all of the above into .claude/settings.json
`

// hookEvent is the subset of the Claude Code hook payload these need.
type hookEvent struct {
	CWD       string `json:"cwd"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath  string `json:"file_path"`
		Content   string `json:"content"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
		Edits     []struct {
			OldString string `json:"old_string"`
			NewString string `json:"new_string"`
		} `json:"edits"`
	} `json:"tool_input"`
}

// edit is one old→new replacement, normalised across Edit / MultiEdit / Write.
type edit struct{ old, new string }

func (e hookEvent) edits() []edit {
	if len(e.ToolInput.Edits) > 0 {
		out := make([]edit, 0, len(e.ToolInput.Edits))
		for _, x := range e.ToolInput.Edits {
			out = append(out, edit{x.OldString, x.NewString})
		}
		return out
	}
	if e.ToolInput.OldString != "" {
		return []edit{{e.ToolInput.OldString, e.ToolInput.NewString}}
	}
	return nil
}

func cmdHook(o opts, args []string) error {
	if len(args) == 0 {
		fmt.Print(hookUsage)
		os.Exit(2)
	}
	if args[0] == "install" {
		return hookInstall(o, args[1:])
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil // fail open
	}
	var ev hookEvent
	if json.Unmarshal(b, &ev) != nil {
		return nil // fail open
	}
	root := ev.CWD
	if root == "" {
		root = o.root
	}
	if r, err := kgraph.DiscoverRoot(root); err == nil {
		root = r
	}

	switch args[0] {
	case "report-stale":
		hookReportStale(o, root)
	default:
		fmt.Fprint(os.Stderr, hookUsage)
		os.Exit(2)
	}
	return nil
}

// ── guard-managed ──────────────────────────────────────────────────────

// ── lint-facts ─────────────────────────────────────────────────────────

// ── guard-output ───────────────────────────────────────────────────────

// ── what-if ────────────────────────────────────────────────────────────

// ── report-stale ───────────────────────────────────────────────────────

// hookReportStale runs at the end of a turn. The failure this whole project
// exists to prevent is a session where a fact was corrected, an answer somebody
// relies on silently moved, and nobody found out until it was quoted.
//
// It reported which DOCUMENTS went stale. There are none, and the question
// underneath it never depended on them: a standing query is a question somebody
// asked, and whether its answer still holds is exactly what a turn can quietly
// break.
func hookReportStale(o opts, root string) {
	p, diags, err := kgraph.Load(root)
	if err != nil {
		return
	}
	env := kgraph.Env{Now: o.now}
	var out []string
	if set, serr := kgraph.ReadStanding(root, kgraph.IndexDirOf(p.Graph)); serr == nil {
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			a, aerr := p.Graph.Ask(set[n], env)
			if aerr != nil || !a.Stale() {
				continue
			}
			// A question nobody has accepted an answer to yet is not news at the end
			// of a turn — same reasoning as `never-rendered` before it.
			if len(a.Drift) == 1 && a.Drift[0] == "never-asked" {
				continue
			}
			line := fmt.Sprintf("  %s [%s]", a.Name, strings.Join(a.Drift, ", "))
			for i, l := range a.Delta.Statements(p.Graph) {
				if i == 3 {
					line += "\n    · …"
					break
				}
				line += "\n    · " + l
			}
			out = append(out, line)
		}
	}
	if errs := kgraph.Errors(diags); len(errs) > 0 {
		out = append(out, fmt.Sprintf("  %d graph error(s) — run `kg scan`", len(errs)))
	}
	if len(out) == 0 {
		return
	}
	sort.Strings(out)
	fmt.Printf("kg: %d document(s) need attention:\n%s\n", len(out), strings.Join(out, "\n"))
}

// ── install ────────────────────────────────────────────────────────────

// hookSpec is one entry to merge into settings.json.
type hookSpec struct {
	event   string
	matcher string
	name    string
}

// installable is the full set, in the order they matter.
var installable = []hookSpec{
	{"Stop", "", "report-stale"},
}

// hookInstall merges the hooks into .claude/settings.json, idempotently, backing
// up first. Writes to the repo's settings by default rather than the user's, so
// a graph-specific hook does not fire in unrelated projects.
func hookInstall(o opts, args []string) error {
	path := filepath.Join(o.root, ".claude", "settings.json")
	if len(args) > 0 {
		path = args[0]
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "kg"
	}

	settings := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(b, &settings) != nil {
			return fmt.Errorf("%s is not valid JSON; not touching it", path)
		}
		if err := os.WriteFile(path+".bak", b, 0o644); err != nil {
			return fmt.Errorf("backup failed: %w", err)
		}
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	var added, already int
	for _, h := range installable {
		cmd := exe + " hook " + h.name
		entries, _ := hooks[h.event].([]any)
		if hookPresent(entries, cmd) {
			already++
			continue
		}
		entry := map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": cmd}},
		}
		if h.matcher != "" {
			entry["matcher"] = h.matcher
		}
		hooks[h.event] = append(entries, entry)
		added++
	}
	settings["hooks"] = hooks

	if added == 0 {
		fmt.Printf("all %d hooks already installed in %s\n", already, path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".kgtmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	fmt.Printf("installed %d hook(s) into %s", added, path)
	if already > 0 {
		fmt.Printf(" (%d already present)", already)
	}
	fmt.Println("\nRestart Claude Code, or run /hooks, to pick them up.")
	return nil
}

// hookPresent reports whether a command is already wired for an event, so a
// re-run is a no-op rather than a duplicate.
func hookPresent(entries []any, cmd string) bool {
	for _, e := range entries {
		m, _ := e.(map[string]any)
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if s, _ := hm["command"].(string); s == cmd {
				return true
			}
		}
	}
	return false
}

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every command the usage text advertises must be dispatched by the switch.
//
// `attest` shipped in the usage string with no `case` for it: the binary printed
// the full help and exited 2 for a command it claimed to have. Same shape as
// TestInstallableNamesAreAllDispatched — a name that exists in one list and not
// the other silently does nothing.
func TestUsageCommandsAreAllDispatched(t *testing.T) {
	var advertised []string
	for _, ln := range strings.Split(usage, "\n") {
		if !strings.HasPrefix(ln, "  kg ") {
			continue
		}
		f := strings.Fields(strings.TrimPrefix(ln, "  kg "))
		if len(f) == 0 || strings.HasPrefix(f[0], "-") {
			continue
		}
		advertised = append(advertised, f[0])
	}
	if len(advertised) < 10 {
		t.Fatalf("usage parse found only %d commands — the parser, not the code, is wrong", len(advertised))
	}
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	seen := map[string]bool{}
	for _, c := range advertised {
		if seen[c] {
			continue
		}
		seen[c] = true
		if !strings.Contains(body, `case "`+c+`"`) && !strings.Contains(body, `"`+c+`",`) {
			t.Errorf("`kg %s` is in the usage text but nothing dispatches it", c)
		}
	}
}

// `kg indexes` is the command somebody runs BECAUSE a marker is wrong, so an
// unknown dialect must be reported and the listing must still come out. Failing
// the whole command would hide every other index at exactly the moment they are
// being looked for.
//
// It is also the only place the dialect key is named to a user. `.kg-index`
// carries a CLOSED key set, so guessing the key wrong is a hard error and there
// is nothing to guess from — this listing is the discovery path.
func TestIndexesReportsDialectsAndSurvivesAnUnknownOne(t *testing.T) {
	root := t.TempDir()
	facts := "# f\n\n```kfacts\n- id: c-x\n  claim: A thing\n  status: asserted\n```\n"
	for dir, marker := range map[string]string{
		"good": "index: good\n",
		"bad":  "index: bad\ndialect: maritime\n",
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, ".kg-index"), []byte(marker), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "f.kfacts.md"), []byte(facts), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out, errOut, err := captureIndexes(t, root)
	if err != nil {
		t.Fatalf("an unknown dialect must not fail the listing: %v", err)
	}
	if !strings.Contains(out, "good") || !strings.Contains(out, "bad") {
		t.Fatalf("both indexes must still be listed, got:\n%s", out)
	}
	if !strings.Contains(out, "legal") {
		t.Errorf("a known dialect must be named in the row, got:\n%s", out)
	}
	// The listing is every dialect the binary knows, so this must not pin the
	// exact string — adding a built-in would break it for no reason.
	if !strings.Contains(errOut, "maritime") || !strings.Contains(errOut, "legal") {
		t.Errorf("the unknown dialect must be reported and say what would be accepted, got:\n%s", errOut)
	}
	// And the footer names the key, or the feature stays undiscoverable.
	if !strings.Contains(out, "dialect: <name>") || !strings.Contains(out, ".kg-index") {
		t.Errorf("the listing must name the marker key, got:\n%s", out)
	}
}

// captureIndexes runs cmdIndexes against root, returning its stdout and stderr.
func captureIndexes(t *testing.T, root string) (string, string, error) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	err := cmdIndexes(opts{root: root})
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	var bo, be bytes.Buffer
	io.Copy(&bo, rOut)
	io.Copy(&be, rErr)
	return bo.String(), be.String(), err
}

// `kg query` validated in the DEFAULT dialect while `kg scan` resolved the
// index's own, so a corpus could hold facts this command then refused to ask
// about: a query naming a dialect's source class came back "unknown source
// class … in the legal dialect" against a corpus that had just scanned clean.
func TestQueryValidatesInTheIndexDialect(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "eng")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".kg-index"),
		[]byte("index: eng\ndialect: engineering\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// `measured` is engineering's top rung and is not a class the legal dialect
	// knows, so this query only parses if the index's dialect is what bound it.
	// Written as the store's export, which is what an index holds now.
	log := `{"op":"assert","id":"s-bench","fields":{"record":"the benchmark run",` +
		`"class":"measured"},"by":"carl","at":"2026-08-30T10:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "facts.jsonl"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}

	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	err := cmdQuery(opts{root: root, index: "eng"}, []string{"source[class=measured]"})
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	var bo, be bytes.Buffer
	io.Copy(&bo, rOut)
	io.Copy(&be, rErr)

	if err != nil {
		t.Fatalf("a query in the index's own dialect must validate: %v\nstderr: %s", err, be.String())
	}
	if !strings.Contains(bo.String(), "s-bench") {
		t.Errorf("the matching fact must come back, got:\n%s", bo.String())
	}
}

// PRECEDENCE, and the refusal. `--store` beats `$KGRAPH_STORE` beats the
// per-index default, and `--store none` refuses every write while saying that it
// refused — a write that silently went to a default database while the operator
// believed they were working from a log is the failure this exists to prevent.
func TestStoreResolutionPrefersTheFlagThenTheEnv(t *testing.T) {
	t.Setenv("KGRAPH_STORE", "/from/the/env.db")
	ref, named := storeRefOf(opts{index: "fence-dispute", store: "/from/the/flag.db"})
	if !named || ref.DSN != "/from/the/flag.db" {
		t.Errorf("the flag must win: %+v", ref)
	}
	ref, named = storeRefOf(opts{index: "fence-dispute"})
	if !named || ref.DSN != "/from/the/env.db" {
		t.Errorf("the env var must be read when no flag is given: %+v", ref)
	}
	t.Setenv("KGRAPH_STORE", "")
	ref, named = storeRefOf(opts{index: "fence-dispute"})
	if named {
		t.Error("nothing named a store, so the default policy must not report itself as named")
	}
	if !strings.Contains(ref.Display(), "fence-dispute") {
		t.Errorf("the default is per-index: %s", ref.Display())
	}
}

func TestStoreNoneRefusesEveryWriteAndSaysWhy(t *testing.T) {
	t.Setenv("KGRAPH_STORE", "")
	_, err := openStore(opts{index: "fence-dispute", store: "none"})
	if err == nil {
		t.Fatal("--store none wrote to a database")
	}
	// The message has to name what it refused and what to do instead, or the
	// operator's next move is to guess.
	for _, want := range []string{"none", "--store", "refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must mention %q, got: %v", want, err)
		}
	}
}

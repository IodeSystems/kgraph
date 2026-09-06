package kgraph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// THE STORE. SQLite is primary; the JSONL is its export.
//
// This INVERTS a settled invariant and the inversion is deliberate — see
// `plan/facts.md`. "Text files authoritative, SQLite derived and disposable" was
// right for two reasons and both stopped applying: `git blame` went with git
// history, and surviving a FILE SYNCER only earns its keep when more than one
// machine writes the same index. It is one machine.
//
// WHAT DID NOT CHANGE is where the database lives. `~/life` syncs over
// Syncthing, and a live SQLite file there is a corruption hazard whether or not
// two machines write it — partial page writes, and `-wal`/`-shm` syncing out of
// step with the database. Single writer removes the write conflict, not the
// syncer. So the database sits OUTSIDE the corpus and the export sits inside it,
// which keeps the property the old invariant was really protecting: what is in
// the corpus directory is text.
//
// DESIGNED FOR POSTGRES, NOT BUILT (USER). Nearly free, because the store holds
// the LOG rather than the graph: one append-only table, and the evaluator stays
// in memory where it already is by decision. The rules that keep it portable are
// on the schema below, and the only genuinely engine-specific thing is the
// placeholder, which is one rebind at this seam rather than per query.

// storeSchema is the whole store.
//
// PORTABILITY RULES, and every one of them is load-bearing rather than taste:
//
//   - `fields` is TEXT holding JSON, never `jsonb` and never `json_extract`.
//     Nothing queries into it in SQL — folding is what turns the log into a
//     graph, and the evaluator is in memory.
//   - `at` is TEXT in RFC3339, never a native timestamp. The engines disagree
//     about timezones and the fold's ordering must not depend on which is
//     underneath.
//   - `seq` is the tie-break for two assertions sharing a timestamp, and it is
//     an explicit column rather than `rowid` or `AUTOINCREMENT`, neither of which
//     Postgres has.
//   - No `INSERT OR REPLACE`. The table is append-only; there is no upsert.
//   - `idx` is the index discriminator, so ONE database can hold several closed
//     graphs. It is what makes a shared Postgres serving a workspace reachable —
//     a file per matter does not need it, and pays only a column for it.
const storeSchema = `
CREATE TABLE IF NOT EXISTS assertion (
  idx    TEXT    NOT NULL DEFAULT '',
  seq    INTEGER NOT NULL,
  op     TEXT    NOT NULL,
  id     TEXT    NOT NULL,
  fields TEXT    NOT NULL DEFAULT '',
  reason TEXT    NOT NULL DEFAULT '',
  into_id TEXT   NOT NULL DEFAULT '',
  note   TEXT    NOT NULL DEFAULT '',
  by_who TEXT    NOT NULL,
  at     TEXT    NOT NULL,
  PRIMARY KEY (idx, seq)
);
`

// storeIndexes is created AFTER the idx migration, not with the table.
// `CREATE INDEX ... ON assertion(idx, id)` cannot run against a store written
// before the column existed, and `CREATE TABLE IF NOT EXISTS` is a no-op there —
// so the two halves have to be separated by the migration that adds the column.
const storeIndexes = `
CREATE INDEX IF NOT EXISTS assertion_id ON assertion(idx, id);
`

// Store is an index's assertion log.
//
// The interface exists so Postgres can slot in without anything above it
// changing. `Apply`, the fold and the ops are storage-agnostic by construction —
// they were written against these two operations before there was a database at
// all, which is what made the swap cheap.
type Store interface {
	All() ([]Assertion, error)
	Append(a Assertion) error
	Close() error
}

type sqlStore struct {
	db *sql.DB
	// idx scopes every read and write. Empty is the DEFAULT index, which is a
	// real index rather than a missing value — the same convention as
	// `DefaultIndex` everywhere else.
	idx string
	// rebind turns `?` into whatever the engine wants. SQLite takes `?`;
	// Postgres takes `$1`. This is the ONLY engine-specific thing in the file,
	// which is the point of keeping the schema as dull as it is.
	rebind func(string) string
}

// StorePath is where an index's database lives, and it is NOT in the corpus.
//
// `~/.kgraph/<index>.db`, outside any synced tree. An index with no name — the
// default one — gets `default.db` rather than a bare dot, because a file called
// `.db` is invisible in exactly the situation somebody is looking for it.
func StorePath(index string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	name := index
	if name == DefaultIndex {
		name = "default"
	}
	// An index name reaches a filename, so it may not reach out of the directory.
	if strings.ContainsAny(name, `/\`) || name == ".." {
		return "", fmt.Errorf("index name %q cannot be a path", index)
	}
	return filepath.Join(home, ".kgraph", name+".db"), nil
}

// ── WHERE a store is ───────────────────────────────────────────────────

// StoreNone is the DSN that names the absence of a store: load from the
// exported log, and refuse every write.
//
// It has to be sayable. "No database" was reachable before only by the file
// happening not to exist, which is a state nobody can ask for and no test can
// assert — and a corpus handed to somebody else is exactly that state. A write
// that silently went to a default database while the operator believed they were
// working from a log is the failure this spells out loud.
const StoreNone = "none"

// StoreRef says WHERE a store is. The zero value is the default policy.
//
// `~/.kgraph/<index>.db` is a POLICY, not the only answer. It is keyed by index
// NAME and nothing else, so two corpora that name an index the same thing share
// a store, silently, and the loser is whichever is read second. A caller that
// keeps its store beside its own corpus says so here instead.
type StoreRef struct {
	// DSN is "" for the default policy, `none`, a filesystem path,
	// `sqlite:<path>`, or `postgres://…`. The scheme picks the engine; a bare
	// path is SQLite, because that is what every existing caller passes.
	DSN string
	// Index is the `idx` discriminator — which index's rows to read when one
	// database holds several. Empty takes the index the FILE is named for, which
	// is what a file-per-matter store wants and costs it nothing. It is what
	// makes one shared database serve a workspace.
	Index string
}

// DefaultStoreRef is the policy: `~/.kgraph/<index>.db`, outside any synced
// tree. See StorePath for why it is not in the corpus.
func DefaultStoreRef(index string) StoreRef { return StoreRef{DSN: defaultDSN(index)} }

func defaultDSN(index string) string {
	path, err := StorePath(index)
	if err != nil {
		// The name is refused again, with its reason, at open time. Carrying the
		// bad name is what lets Display print what was actually asked for.
		return index
	}
	return path
}

// IsNone reports whether the ref names no store at all.
func (r StoreRef) IsNone() bool { return r.DSN == StoreNone }

// Display is the ref as a person should read it — what `kg indexes` prints, so
// which store an index resolved to is visible rather than guessed at.
func (r StoreRef) Display() string {
	if r.DSN == "" {
		return "(default)"
	}
	return r.DSN
}

// Path is the filesystem path a ref names, and whether it names one at all.
//
// `none` and a Postgres DSN name no file. Exists so a caller can report whether
// the database is actually THERE — the shadowing failure this type addresses is
// invisible until somebody can see which store an index resolved to and whether
// it holds anything.
func (r StoreRef) Path() (string, bool) {
	switch {
	case r.DSN == StoreNone:
		return "", false
	case strings.HasPrefix(r.DSN, "postgres://"), strings.HasPrefix(r.DSN, "postgresql://"):
		return "", false
	case strings.HasPrefix(r.DSN, "sqlite:"):
		return expandHome(strings.TrimPrefix(r.DSN, "sqlite:")), true
	case r.DSN == "":
		p, err := StorePath(DefaultIndex)
		return p, err == nil
	default:
		return expandHome(r.DSN), true
	}
}

// OpenStoreRef opens the store a ref names.
//
// A NIL STORE AND A NIL ERROR IS THE `none` ANSWER, not a failure: load from the
// export, and let the write path refuse. Every caller that writes must check for
// it — see `Store` on why the absence is a state worth being able to name.
func OpenStoreRef(ref StoreRef) (Store, error) {
	dsn := ref.DSN
	switch {
	case dsn == StoreNone:
		return nil, nil
	case dsn == "":
		// The zero value is the default policy for the default index. A caller
		// naming an index goes through DefaultStoreRef.
		p, err := StorePath(DefaultIndex)
		if err != nil {
			return nil, err
		}
		return openRefPath(p, ref.Index)
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return OpenPostgres(dsn, ref.Index)
	case strings.HasPrefix(dsn, "sqlite:"):
		return openRefPath(expandHome(strings.TrimPrefix(dsn, "sqlite:")), ref.Index)
	default:
		return openRefPath(expandHome(dsn), ref.Index)
	}
}

// openRefPath opens a file store, honouring an explicit index and otherwise
// taking the one the filename names.
func openRefPath(path, idx string) (Store, error) {
	if idx != "" {
		return OpenStoreIn(path, idx)
	}
	return OpenStore(path)
}

// expandHome resolves a leading `~`, because a DSN is typed by a person as often
// as it is composed by a program and `~/.kgraph/x.db` reaching a file called `~`
// is a silent wrong answer.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
}

// OpenStore opens or creates an index's store.
func OpenStore(path string) (Store, error) { return openStoreIdx(path, IndexOfStorePath(path)) }

// IndexOfStorePath is the index a store FILE is named for, and it is the single
// source of that derivation.
//
// Both the migration and the reader must agree about it or the store goes
// silently empty: stamping rows with the file's index and then reading with the
// empty one returns nothing, with no error and no missing file. `default.db` is the
// DEFAULT index, whose name is the empty string.
func IndexOfStorePath(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".db")
	if name == "default" {
		return DefaultIndex
	}
	return name
}

// OpenStoreIn opens a store scoped to one index inside it. `""` is the default
// index. A file per matter passes `""` and pays nothing; a shared database
// passes the index it means.
func OpenStoreIn(path, idx string) (Store, error) { return openStoreIdx(path, idx) }

func openStoreIdx(path, idx string) (Store, error) {
	// 0700 / 0600, the same treatment the token store gets and for a stronger
	// reason: this is a live legal and medical corpus, and it is now the PRIMARY
	// copy rather than a derived index that could be deleted and rebuilt. SQLite
	// creates the file 0644 if left alone, so it is created here first.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		f.Close()
	} else if !os.IsExist(err) {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One writer, so one connection. It also makes the append ordering the fold
	// relies on trivially true rather than a thing to reason about.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(storeSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// SQLITE ONLY. `PRAGMA table_info` has no Postgres equivalent, and a Postgres
	// table is created with `idx` from the start — there is no pre-idx shape to
	// migrate, because the first Postgres store will be made by this code.
	if err := migrateIdx(db, path); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(storeIndexes); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &sqlStore{db: db, idx: idx, rebind: func(q string) string { return q }}, nil
}

// migrateIdx adds the `idx` discriminator to a store written before it existed.
//
// THIS IS WHERE THE DISCRIMINATOR CAN GO SILENTLY WRONG, and the failure has no
// error in it: `ADD COLUMN idx TEXT NOT NULL DEFAULT ”` leaves every existing
// row under the EMPTY index, so a scoped read of a named index returns nothing
// and the corpus reports itself empty — no error, no missing file, just a graph
// with no nodes in it.
//
// So existing rows are STAMPED with the index the FILE was named for, which
// `<index>.db` knows, and a filename that yields no name is left alone rather
// than guessed at: the default index IS the empty string, so a `default.db`
// wants exactly the empty stamp it already has.
func migrateIdx(db *sql.DB, path string) error {
	var has bool
	rows, err := db.Query(`PRAGMA table_info(assertion)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "idx" {
			has = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if !has {
		if _, err := db.Exec(`ALTER TABLE assertion ADD COLUMN idx TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	// Stamp rows that predate the column. `default.db` is the DEFAULT index,
	// whose name is the empty string, so it is already correct and is skipped.
	name := IndexOfStorePath(path)
	if name == DefaultIndex {
		return nil
	}
	_, err = db.Exec(`UPDATE assertion SET idx = ? WHERE idx = ''`, name)
	return err
}

// OpenPostgres opens a store in a Postgres database.
//
// THE FOLD DOES NOT CHANGE, and that is what made this cheap: the store holds
// the LOG rather than the graph, so there is one append-only table and the
// evaluator stays in memory where it already is by decision. Everything
// engine-specific is here — the driver, the placeholder, and the fact that a
// server-side database has no file permissions to set.
//
// `idx` is the index this handle reads and writes. Unlike a file store there is
// no filename to derive it from, so a shared database MUST be told, or one
// matter would silently read another's log. Empty is the default index, which is
// a real index and not a missing value.
func OpenPostgres(dsn, idx string) (Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	// Not capped at one connection, unlike SQLite. That cap was what made
	// `MAX(seq)+1` exactly right; here it is not, which is what the primary key
	// and the retry in `Append` are for.
	if _, err := db.Exec(storeSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if _, err := db.Exec(storeIndexes); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	return &sqlStore{db: db, idx: idx, rebind: pgRebind}, nil
}

// pgRebind turns `?` placeholders into `$1`, `$2`, …
//
// THE ONLY ENGINE-SPECIFIC THING IN EVERY QUERY, which is the whole point of
// keeping the schema as dull as it is: no `jsonb`, no `json_extract`, no native
// timestamps, no `AUTOINCREMENT`, no upsert.
func pgRebind(q string) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

func (s *sqlStore) Close() error { return s.db.Close() }

// All returns the log in fold order: by `at`, then by `seq`.
//
// Ordered HERE rather than left to the fold, so the two cannot disagree about
// what "the log" means. The fold sorts again — it also serves callers holding a
// slice from somewhere else — and sorting an already-sorted slice is free.
func (s *sqlStore) All() ([]Assertion, error) {
	rows, err := s.db.Query(s.rebind(
		`SELECT op, id, fields, reason, into_id, note, by_who, at
		   FROM assertion WHERE idx = ? ORDER BY at, seq`), s.idx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Assertion
	for rows.Next() {
		var a Assertion
		var fields string
		if err := rows.Scan(&a.Op, &a.ID, &fields, &a.Reason, &a.Into, &a.Note, &a.By, &a.At); err != nil {
			return nil, err
		}
		if fields != "" {
			if err := json.Unmarshal([]byte(fields), &a.Fields); err != nil {
				return nil, fmt.Errorf("assertion %q: %w", a.ID, err)
			}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Append adds one assertion. There is no update and no delete: the table is the
// log, and a correction is a later line rather than an edit to an earlier one.
func (s *sqlStore) Append(a Assertion) error {
	if err := a.validate(); err != nil {
		return err
	}
	var fields string
	if len(a.Fields) > 0 {
		b, err := json.Marshal(a.Fields)
		if err != nil {
			return err
		}
		fields = string(b)
	}
	// `seq` is assigned rather than auto-generated, because Postgres and SQLite
	// spell auto-generation differently and the value only has to be monotonic
	// within one store. Under one writer with one connection, a MAX+1 is exactly
	// that.
	var next int64
	if err := s.db.QueryRow(s.rebind(
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM assertion WHERE idx = ?`), s.idx).Scan(&next); err != nil {
		return err
	}
	// `PRIMARY KEY (idx, seq)` makes a lost race a CONSTRAINT VIOLATION rather
	// than a silent overwrite. Under SQLite with one connection the MAX+1 is
	// exact and this never fires; under a shared Postgres two appends can read
	// the same MAX, and being refused is the outcome worth having.
	// RETRIED, because `MAX(seq)+1` is exact under SQLite's single connection and
	// is a RACE under a shared Postgres: two appends read the same MAX and write
	// the same seq. `PRIMARY KEY (idx, seq)` turns that into a constraint
	// violation rather than a silent overwrite — losing an assertion is the one
	// outcome an append-only log may not have — and this turns the violation into
	// a re-read. Bounded: a caller that cannot get a number after this many tries
	// is contending with something that is not going to stop.
	for attempt := 0; ; attempt++ {
		_, err := s.db.Exec(s.rebind(
			`INSERT INTO assertion (idx, seq, op, id, fields, reason, into_id, note, by_who, at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			s.idx, next, string(a.Op), a.ID, fields, a.Reason, a.Into, a.Note, a.By, a.At)
		if err == nil || attempt >= seqRetries || !isDuplicateSeq(err) {
			return err
		}
		if rerr := s.db.QueryRow(s.rebind(
			`SELECT COALESCE(MAX(seq), 0) + 1 FROM assertion WHERE idx = ?`), s.idx).Scan(&next); rerr != nil {
			return rerr
		}
	}
}

// seqRetries bounds the re-read above.
const seqRetries = 8

// isDuplicateSeq reports whether an insert lost the race for a sequence number.
//
// Matched on the message rather than on a driver error type, deliberately: this
// package speaks `database/sql` and knows about two engines through one
// interface, and importing a driver's error types here would put an engine back
// into the seam that exists to keep them out.
func isDuplicateSeq(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "duplicate key") || // postgres
		strings.Contains(m, "unique constraint") // sqlite
}

// ── the export ─────────────────────────────────────────────────────────

// ExportJSONL writes the store out as the log, one assertion per line.
//
// This is what lives in the corpus and what goes to git if anybody wants it
// there: an audit trail, a backup, and the portable form. It is a projection of
// the store and never the other way round — which is the whole inversion, stated
// as code.
func ExportJSONL(st Store, path string) (int, error) {
	log, err := st.All()
	if err != nil {
		return 0, err
	}
	var b strings.Builder
	b.WriteString("// kgraph assertion log — an EXPORT of the store, not the store.\n")
	b.WriteString("// Every line records who asserted what and when. Rebuild with `kg import`.\n")
	for _, a := range log {
		line, merr := json.Marshal(a)
		if merr != nil {
			return 0, merr
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	// WRITTEN WHOLE OR NOT AT ALL. `os.WriteFile` truncates and then writes, so
	// a reader arriving mid-write sees a short log and a second writer arriving
	// mid-write interleaves with the first. Neither is hypothetical for this
	// file: it is a PROJECTION re-emitted after every write, consumers export
	// after each one, and more than one process writes a corpus by design — a
	// long-lived daemon and an ordinary CLI on the same box, or an old and a new
	// binary during a service upgrade.
	//
	// What that costs is not a retry. This file is the portable form and the
	// audit trail — the thing a clone gets and the thing git diffs — so a
	// truncated one is lost history that looks like history.
	//
	// Same directory, because rename is only atomic within a filesystem; a temp
	// under /tmp would silently degrade to copy-and-truncate across a mount.
	tmp, terr := os.CreateTemp(filepath.Dir(path), ".facts-*.jsonl")
	if terr != nil {
		return 0, terr
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once the rename succeeds
	if _, werr := tmp.WriteString(b.String()); werr != nil {
		_ = tmp.Close()
		return 0, werr
	}
	// FSYNC BEFORE THE RENAME. Without it the rename can land before the bytes
	// do, and a machine that loses power between them leaves an empty file where
	// a complete log used to be — the failure this whole function exists to
	// avoid, arriving by a different route.
	if serr := tmp.Sync(); serr != nil {
		_ = tmp.Close()
		return 0, serr
	}
	if cerr := tmp.Close(); cerr != nil {
		return 0, cerr
	}
	if cerr := os.Chmod(tmp.Name(), 0o644); cerr != nil {
		return 0, cerr
	}
	return len(log), os.Rename(tmp.Name(), path)
}

// ImportJSONL replays an exported log into a store.
//
// The inverse of the export, and the reason the export is worth having: a store
// that cannot be rebuilt from its text is a store whose text is decoration. It
// APPENDS rather than replacing, so importing into a non-empty store is a merge
// — which is what recovering half a corpus from a backup actually looks like.
func ImportJSONL(st Store, path string) (int, error) {
	log, err := ReadAssertionLog(path)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range log {
		if err := st.Append(a); err != nil {
			return n, fmt.Errorf("%s: %w", path, err)
		}
		n++
	}
	return n, nil
}

// LastFields returns the authored fields of the most recent `assert` of id.
//
// THIS IS HOW A WRITE OP AMENDS A FACT, and it is why the log holds authored
// vocabulary rather than a typed struct. An amendment — add an alias, correct a
// moved `doc:` — needs the node's other fields to come along, because an assert
// REPLACES rather than merges. Reconstructing them from the built `Node` would
// be the parser run backwards, which is the second implementation the fold and
// the migration both refuse; reading the last line gives them back exactly as
// they were written.
//
// The returned map is a copy, so a caller may modify it freely.
func LastFields(st Store, id string) (map[string]any, bool, error) {
	log, err := st.All()
	if err != nil {
		return nil, false, err
	}
	var found map[string]any
	for _, a := range log {
		if a.ID == id && a.Op == OpAssert {
			found = a.Fields
		}
	}
	if found == nil {
		return nil, false, nil
	}
	out := make(map[string]any, len(found))
	for k, v := range found {
		out[k] = v
	}
	return out, true, nil
}

// Amend re-asserts a fact with one or more fields changed.
//
// The general form of "the corpus was right about this and wrong about that":
// a moved exhibit, an alias somebody added, a class reconsidered. It is a NEW
// LINE rather than an edit, so what the corpus used to say survives — and the
// note is where the reason goes, which is the whole reason a repair is worth
// recording rather than performing.
func Amend(st Store, dl Dialect, id string, change map[string]any, by, note string) (*WriteResult, error) {
	fields, ok, err := LastFields(st, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no assertion of %q to amend", id)
	}
	for k, v := range change {
		if v == nil {
			// A nil value DROPS the field, which is how a fact stops claiming
			// something. An assert replaces, so absence is expressible and has to be.
			delete(fields, k)
			continue
		}
		fields[k] = v
	}
	return Apply(st, dl, Assertion{Op: OpAssert, ID: id, Fields: fields, By: by, Note: note})
}

// ── the standing-query projection ──────────────────────────────────────

// queriesSchema is `kgraph_query`: what was asked, the answer on record, and
// whether it still holds.
//
// A PROJECTION, and the one that is safe to have. `kgraph_node`/`kgraph_edge`
// were refused because they would be a second copy of the FOLD, which can drift
// from the fold silently. This is different in the way that matters: a pin is
// AUTHORED — somebody resolved this question, accepted the answer, and signed it
// — so the table is a record rather than a derivation. The one computed column,
// `invalidated`, cannot disagree with the fold, only lag it.
//
// It exists for consumers. caselit and aw4 both want "is this answer still
// good?" without linking kgraph, folding the log and re-evaluating in memory.
//
// `drift` carries the REASONS, not just the flag. `Ask` distinguishes
// never-asked · query-changed · results-changed · dialect-changed ·
// source-changed, and collapsing five states into one boolean throws away the
// only part a consumer can act on.
const queriesSchema = `
CREATE TABLE IF NOT EXISTS kgraph_query (
  idx          TEXT    NOT NULL,
  name         TEXT    NOT NULL,
  query_group  TEXT    NOT NULL DEFAULT '',
  purpose      TEXT    NOT NULL DEFAULT '',
  query        TEXT    NOT NULL,
  query_hash   TEXT    NOT NULL DEFAULT '',
  dialect_hash TEXT    NOT NULL DEFAULT '',
  set_hash     TEXT    NOT NULL DEFAULT '',
  row_count    INTEGER NOT NULL DEFAULT 0,
  pin          TEXT    NOT NULL DEFAULT '',
  invalidated  INTEGER NOT NULL DEFAULT 0,
  drift        TEXT    NOT NULL DEFAULT '',
  answered_at  TEXT    NOT NULL DEFAULT '',
  answered_by  TEXT    NOT NULL DEFAULT '',
  -- computed_at is WHEN invalidated was decided. A consumer comparing against a
  -- newer log must treat the flag as unknown rather than fresh: a stale flag is
  -- a lie nothing downstream can detect. Same distinction as never-asked.
  computed_at  TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (idx, name)
);
`

// StandingRow is one standing query as the projection records it.
type StandingRow struct {
	Name        string
	Group       string
	Purpose     string
	Query       string
	QueryHash   string
	DialectHash string
	SetHash     string
	Count       int
	Pin         string // the pin as JSON, or "" when never asked
	Invalidated bool
	Drift       []string
	At          string
	By          string
}

// QueryWriter is the optional half of a store that can hold the projection.
//
// Kept OFF `Store`, which stays the three methods the fold needs. A store that
// cannot project is still a store — the log is the thing — and an engine that
// grows the table later does not change the interface everything else is
// written against.
type QueryWriter interface {
	WriteQueries(rows []StandingRow, computedAt string) error
}

// WriteQueries replaces this index's projection.
//
// A WHOLE-INDEX REPLACE, not an upsert per row: a standing query that has been
// DELETED must leave the table, or a consumer keeps filtering on an answer to a
// question nobody asks any more. Scoped by `idx`, so one index rewriting its own
// questions never touches another's.
//
// This table is never read back as authority. It is rebuildable from
// `standing.yaml` plus the fold, written only by `kg ask`, and a consumer that
// wants to ACCEPT an answer goes through the op exactly as a writer goes through
// `kg assert`.
func (s *sqlStore) WriteQueries(rows []StandingRow, computedAt string) error {
	if _, err := s.db.Exec(queriesSchema); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(s.rebind(`DELETE FROM kgraph_query WHERE idx = ?`), s.idx); err != nil {
		return err
	}
	for _, r := range rows {
		inval := 0
		if r.Invalidated {
			inval = 1
		}
		if _, err := tx.Exec(s.rebind(
			`INSERT INTO kgraph_query (idx, name, query_group, purpose, query, query_hash,
			   dialect_hash, set_hash, row_count, pin, invalidated, drift, answered_at,
			   answered_by, computed_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			s.idx, r.Name, r.Group, r.Purpose, r.Query, r.QueryHash, r.DialectHash,
			r.SetHash, r.Count, r.Pin, inval, strings.Join(r.Drift, " "), r.At, r.By,
			computedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReadQueries returns the projection for this index, for a test or a caller that
// wants it without SQL of its own.
func (s *sqlStore) ReadQueries() ([]StandingRow, error) {
	if _, err := s.db.Exec(queriesSchema); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(s.rebind(
		`SELECT name, query_group, purpose, query, query_hash, dialect_hash, set_hash,
		        row_count, pin, invalidated, drift, answered_at, answered_by
		   FROM kgraph_query WHERE idx = ? ORDER BY name`), s.idx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StandingRow
	for rows.Next() {
		var r StandingRow
		var inval int
		var drift string
		if err := rows.Scan(&r.Name, &r.Group, &r.Purpose, &r.Query, &r.QueryHash,
			&r.DialectHash, &r.SetHash, &r.Count, &r.Pin, &inval, &drift, &r.At, &r.By); err != nil {
			return nil, err
		}
		r.Invalidated = inval == 1
		if drift != "" {
			r.Drift = strings.Fields(drift)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

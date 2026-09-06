package kgraph

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/iodesystems/gwag/v2/gw/gat"
)

// A single persistent daemon serves every project. Requests carry a `dir` and
// the daemon resolves it to a (root, branch) scope, so routing is implicit from
// the caller's working directory — no flags, no per-project process.
//
// Handlers register through gat, so the same set yields REST, GraphQL and gRPC
// from one definition.

const DefaultAddr = "127.0.0.1:7421"

type Daemon struct {
	Reg *Registry
	Now func() string // injectable clock, so tests are not date-dependent
}

func NewDaemon() *Daemon {
	return &Daemon{Reg: NewRegistry(), Now: func() string { return time.Now().Format("2006-01-02") }}
}

// ── wire types ─────────────────────────────────────────────────────────

// Target selects which project and branch a request means. EXPORTED on purpose. huma skips unexported fields when it discovers
// request parameters, and an embedded field takes its name from its type — so
// embedding a lowercase `scope` would silently drop `dir` and `now` from both the
// OpenAPI document and the runtime binding. gwag's gat refuses to mount if that
// happens; see docs/gat.md "Trap: parameters on an unexported field".
type Target struct {
	Dir string `query:"dir" doc:"Any directory inside the project. The daemon resolves it to a project root and branch."`
	Now string `query:"now" doc:"Override the clock for @now (YYYY-MM-DD)."`
}

type specIn struct {
	Target
	Spec string `query:"spec" doc:"Spec name, basename, or repo-relative path. Omit for all."`
}

type SpecState struct {
	Spec    string   `json:"spec"`
	State   []string `json:"state"`
	Outputs []string `json:"outputs"`
}

type DeltaReport struct {
	Spec  string   `json:"spec"`
	State []string `json:"state"`
	Delta []string `json:"delta,omitempty"`
}

// OutputText is the current text of one output.
type OutputText struct {
	Path string `json:"path"`
	Text string `json:"text"`
}

// GraphNode and GraphEdge are the wire shape for the graph view.
type GraphNode struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Label   string `json:"label"`
	Matched bool   `json:"matched"`
	Class   string `json:"class,omitempty"`
}

type GraphEdge struct {
	Src  string `json:"src"`
	Dst  string `json:"dst"`
	Type string `json:"type"`
}

func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

type QueryRow struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Body   string `json:"body"`
}

// ── handlers ───────────────────────────────────────────────────────────

func (d *Daemon) scope(dir, now string) (*Scope, Env, error) {
	// No silent fallback to the daemon's own cwd. That default is exactly what
	// let a broken `dir` binding look healthy during testing: requests resolved
	// to whichever project the daemon happened to be started in, and only looked
	// right because it was the same one. A missing scope must be loud.
	if dir == "" {
		return nil, Env{}, huma.Error400BadRequest(
			"dir is required — it is how the daemon resolves which project and branch you mean")
	}
	sc, err := d.Reg.Get(dir)
	if err != nil {
		return nil, Env{}, huma.Error400BadRequest(err.Error())
	}
	if now == "" {
		now = d.Now()
	}
	return sc, Env{Now: now, Named: sc.Project().Named}, nil
}

// specs resolves the declared sets a request names, or all of them.
//
// Takes declared SETS rather than specs since 2026-09-01: a standing group is a
// declaration exactly as a spec document was, and this is the only place the
// daemon had to know which.
func (d *Daemon) specs(sc *Scope, name string) ([]DeclaredSet, error) {
	all := sc.Project().DeclaredSets()
	if name == "" {
		return all, nil
	}
	var hits []DeclaredSet
	for _, s := range all {
		if s.Name == name || s.Path == name ||
			strings.TrimSuffix(filepath.Base(s.Path), SpecSuffix) == name {
			hits = append(hits, s)
		}
	}
	if len(hits) == 0 {
		var known []string
		for _, s := range all {
			known = append(known, s.Name)
		}
		sort.Strings(known)
		return nil, huma.Error404NotFound(fmt.Sprintf(
			"no declared set %q — known: %s", name, strings.Join(known, ", ")))
	}
	return hits, nil
}

func (d *Daemon) Register(api huma.API, g *gat.Gateway) error {
	gat.Register(api, g, huma.Operation{
		OperationID: "listScopes", Method: http.MethodGet, Path: "/scopes",
		Summary: "Projects and branches the daemon holds. Passing `dir` also loads that one.",
	}, func(ctx context.Context, in *struct {
		Dir string `query:"dir" doc:"Load and include this project's scope."`
	}) (*struct {
		Body struct {
			Scopes []*Scope `json:"scopes"`
		}
	}, error) {
		if in.Dir != "" {
			if _, _, err := d.scope(in.Dir, ""); err != nil {
				return nil, err
			}
		}
		out := &struct {
			Body struct {
				Scopes []*Scope `json:"scopes"`
			}
		}{}
		out.Body.Scopes = d.Reg.Scopes()
		return out, nil
	})

	gat.Register(api, g, huma.Operation{
		OperationID: "backlog", Method: http.MethodGet, Path: "/backlog",
		Summary: "Documents in the project and what has been read from each. `unread` means no source cites it.",
	}, func(ctx context.Context, in *Target) (*struct {
		Body struct {
			Docs []DocStatus `json:"docs"`
		}
	}, error) {
		sc, _, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		docs, berr := Backlog(sc.Root, sc.Project().Index, sc.Project().Graph)
		if berr != nil {
			return nil, huma.Error400BadRequest(berr.Error())
		}
		out := &struct {
			Body struct {
				Docs []DocStatus `json:"docs"`
			}
		}{}
		out.Body.Docs = docs
		return out, nil
	})

	// Read-only: it emits a prompt. The facts an agent writes from it go in
	// through `kg assert` — which folds, builds and refuses anything that would
	// not load — so validation is no longer a separate step somebody has to
	// remember. kgraph still writes no facts of its own.
	gat.Register(api, g, huma.Operation{
		OperationID: "extract", Method: http.MethodGet, Path: "/extract",
		Summary: "The extraction prompt for one document: its body, what is already read from it, and the open evidence questions it might close.",
	}, func(ctx context.Context, in *struct {
		Target
		Doc string `query:"doc" required:"true" doc:"project-relative document path"`
	}) (*struct{ Body *Extraction }, error) {
		sc, env, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		x, xerr := sc.Project().Graph.Extract(sc.Root, in.Doc, env)
		if xerr != nil {
			return nil, huma.Error400BadRequest(xerr.Error())
		}
		return &struct{ Body *Extraction }{Body: x}, nil
	})

	// Read-only on purpose. `kg source lock` is the only writer, and it is a
	// deliberate act: recording a hash asserts the document was re-read, and no
	// remote caller should be able to assert that on the author's behalf.
	gat.Register(api, g, huma.Operation{
		OperationID: "sources", Method: http.MethodGet, Path: "/sources",
		Summary: "The recorded version of each evidence document, and which have moved since.",
	}, func(ctx context.Context, in *Target) (*struct {
		Body struct {
			Sources []SourceStatus `json:"sources"`
		}
	}, error) {
		sc, _, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		l, lerr := ReadLocks(sc.Root, sc.Project().Graph)
		if lerr != nil {
			return nil, huma.Error400BadRequest(lerr.Error())
		}
		out := &struct {
			Body struct {
				Sources []SourceStatus `json:"sources"`
			}
		}{}
		out.Body.Sources, _ = sc.Project().Graph.SourceStatuses(sc.Root, l)
		return out, nil
	})

	gat.Register(api, g, huma.Operation{
		OperationID: "scan", Method: http.MethodGet, Path: "/scan",
		Summary: "Parse everything and report problems, including spec queries that resolve to nothing.",
	}, func(ctx context.Context, in *Target) (*struct {
		Body struct {
			Scope    *Scope   `json:"scope"`
			Problems []string `json:"problems"`
		}
	}, error) {
		sc, env, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		var problems []string
		for _, dg := range sc.Diags() {
			problems = append(problems, dg.String())
		}
		for _, s := range sc.Project().DeclaredSets() {
			pins, err := sc.Project().Graph.Resolve(s, env)
			if err != nil {
				problems = append(problems, s.Path+": "+err.Error())
				continue
			}
			for name, pin := range pins {
				if pin.Count == 0 {
					problems = append(problems, fmt.Sprintf(
						"%s: query %q resolves to 0 rows — check the hop direction", s.Path, name))
				}
			}
		}
		out := &struct {
			Body struct {
				Scope    *Scope   `json:"scope"`
				Problems []string `json:"problems"`
			}
		}{}
		out.Body.Scope, out.Body.Problems = sc, problems
		return out, nil
	})

	// STATUS AND DIFF ARE OVER STANDING QUERIES. They reported which DOCUMENTS
	// were stale, read from each spec's sealed managed block; there are no
	// documents and no managed blocks. The question survives the answer: a
	// standing query is one somebody asked, and `Ask` returns its rows and its
	// drift together so the two cannot come from different resolutions.
	gat.Register(api, g, huma.Operation{
		OperationID: "status", Method: http.MethodGet, Path: "/status",
		Summary: "Which standing answers have moved, and why.",
	}, func(ctx context.Context, in *specIn) (*struct {
		Body struct {
			Answers []AnswerState `json:"answers"`
		}
	}, error) {
		sc, env, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		if err := sc.Err(); err != nil {
			return nil, huma.Error409Conflict(err.Error())
		}
		out := &struct {
			Body struct {
				Answers []AnswerState `json:"answers"`
			}
		}{}
		out.Body.Answers = []AnswerState{}
		for _, a := range d.answers(sc, env, in.Spec) {
			out.Body.Answers = append(out.Body.Answers, AnswerState{
				Name: a.Name, Rows: a.Pin.Count, Drift: a.Drift})
		}
		return out, nil
	})

	gat.Register(api, g, huma.Operation{
		OperationID: "diff", Method: http.MethodGet, Path: "/diff",
		Summary: "What moved since each answer was accepted, as explicit statements.",
	}, func(ctx context.Context, in *specIn) (*struct {
		Body struct {
			Changed []DeltaReport `json:"changed"`
		}
	}, error) {
		sc, env, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		if err := sc.Err(); err != nil {
			return nil, huma.Error409Conflict(err.Error())
		}
		out := &struct {
			Body struct {
				Changed []DeltaReport `json:"changed"`
			}
		}{}
		out.Body.Changed = []DeltaReport{}
		for _, a := range d.answers(sc, env, in.Spec) {
			if !a.Stale() {
				continue
			}
			out.Body.Changed = append(out.Body.Changed, DeltaReport{
				Spec: a.Name, State: a.Drift, Delta: a.Delta.Statements(sc.Project().Graph)})
		}
		return out, nil
	})

	gat.Register(api, g, huma.Operation{
		OperationID: "whatIf", Method: http.MethodPost, Path: "/what-if",
		Summary: "Blast radius of a fact before committing it. Writes nothing.",
	}, func(ctx context.Context, in *struct {
		Target
		Body struct {
			Facts string `json:"facts" doc:"A YAML fragment of entries."`
		}
	}) (*struct {
		Body struct {
			Affected []DeltaReport `json:"affected"`
		}
	}, error) {
		sc, env, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		if err := sc.Err(); err != nil {
			return nil, huma.Error409Conflict(err.Error())
		}
		scratch, diags := sc.Project().Graph.WhatIf(in.Body.Facts)
		if scratch == nil {
			return nil, huma.Error422UnprocessableEntity(diagText(diags))
		}
		out := &struct {
			Body struct {
				Affected []DeltaReport `json:"affected"`
			}
		}{}
		out.Body.Affected = []DeltaReport{}
		for _, s := range sc.Project().DeclaredSets() {
			dl, err := DiffGraphs(sc.Project().Graph, scratch, s, env)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			if len(dl.Sets) == 0 {
				continue
			}
			out.Body.Affected = append(out.Body.Affected, DeltaReport{
				Spec: dl.Spec, State: dl.State, Delta: dl.Statements(scratch)})
		}
		return out, nil
	})

	gat.Register(api, g, huma.Operation{
		OperationID: "query", Method: http.MethodGet, Path: "/query",
		Summary: "Resolve a query against the graph.",
	}, func(ctx context.Context, in *struct {
		Target
		Q string `query:"q" required:"true" doc:"kgraph query DSL."`
	}) (*struct {
		Body struct {
			Count int        `json:"count"`
			Rows  []QueryRow `json:"rows"`
		}
	}, error) {
		sc, env, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		q, perr := ParseQuery(in.Q)
		if perr != nil {
			return nil, huma.Error422UnprocessableEntity(perr.Error())
		}
		if errs := ValidateQuery(q); len(errs) > 0 {
			return nil, huma.Error422UnprocessableEntity(errs[0].Error())
		}
		ids, err := sc.Project().Graph.Eval(q, env)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		out := &struct {
			Body struct {
				Count int        `json:"count"`
				Rows  []QueryRow `json:"rows"`
			}
		}{}
		out.Body.Rows = []QueryRow{}
		for _, id := range ids {
			n := sc.Project().Graph.Nodes[id]
			out.Body.Rows = append(out.Body.Rows, QueryRow{id, string(n.Kind), string(n.Status), n.Body})
		}
		out.Body.Count = len(out.Body.Rows)
		return out, nil
	})

	gat.Register(api, g, huma.Operation{
		OperationID: "variants", Method: http.MethodGet, Path: "/variants",
		Summary: "Open questions a document rests on, and what each declared answer would change.",
	}, func(ctx context.Context, in *specIn) (*struct {
		Body struct {
			Reports []VariantReport `json:"reports"`
		}
	}, error) {
		sc, env, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		if err := sc.Err(); err != nil {
			return nil, huma.Error409Conflict(err.Error())
		}
		specs, err := d.specs(sc, in.Spec)
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Reports []VariantReport `json:"reports"`
			}
		}{}
		out.Body.Reports = []VariantReport{}
		for _, sp := range specs {
			rs, verr := sc.Project().Graph.Variants(sp, env)
			if verr != nil {
				return nil, huma.Error400BadRequest(verr.Error())
			}
			out.Body.Reports = append(out.Body.Reports, rs...)
		}
		return out, nil
	})

	// The UI needs the shape of a result, not only its ids: a node with its
	// rendered form for the inspector, and the induced subgraph for the map.
	gat.Register(api, g, huma.Operation{
		OperationID: "projects", Method: http.MethodGet, Path: "/projects",
		Summary: "Projects the console can open: remembered, loaded, and discovered under a path.",
	}, func(ctx context.Context, in *struct {
		Under string `query:"under" doc:"Search under this directory. Defaults to the remembered set plus $HOME."`
		Depth int    `query:"depth" doc:"How deep to walk." default:"8"`
	}) (*struct {
		Body struct {
			Projects []Candidate `json:"projects"`
			Searched []string    `json:"searched"`
		}
	}, error) {
		loaded := map[string]bool{}
		for _, sc := range d.Reg.Scopes() {
			loaded[sc.Root] = true
		}
		seen := map[string]Candidate{}
		var searched []string

		add := func(c Candidate) {
			c.Loaded = loaded[c.Root]
			if prev, ok := seen[c.Root]; !ok || c.Facts > prev.Facts {
				seen[c.Root] = c
			}
		}
		// Remembered roots first: the console should not be empty after a restart.
		for _, root := range d.Reg.Known() {
			cs, err := Discover(root, 1)
			if err != nil || len(cs) == 0 {
				add(Candidate{Root: root, Branch: Branch(root)})
				continue
			}
			for _, c := range cs {
				add(c)
			}
		}
		roots := []string{}
		if in.Under != "" {
			roots = append(roots, in.Under)
		} else if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, home)
		}
		for _, r := range roots {
			searched = append(searched, r)
			cs, err := Discover(r, in.Depth)
			if err != nil {
				continue
			}
			for _, c := range cs {
				add(c)
			}
		}
		out := &struct {
			Body struct {
				Projects []Candidate `json:"projects"`
				Searched []string    `json:"searched"`
			}
		}{}
		out.Body.Projects = []Candidate{}
		out.Body.Searched = searched
		for _, c := range seen {
			out.Body.Projects = append(out.Body.Projects, c)
		}
		sort.Slice(out.Body.Projects, func(i, j int) bool {
			a, b := out.Body.Projects[i], out.Body.Projects[j]
			if a.Loaded != b.Loaded {
				return a.Loaded // what is already open sorts first
			}
			return a.Root < b.Root
		})
		return out, nil
	})

	gat.Register(api, g, huma.Operation{
		OperationID: "named", Method: http.MethodGet, Path: "/named",
		Summary: "Names in the shared query library, for the console.",
	}, func(ctx context.Context, in *struct {
		Dir string `query:"dir" doc:"Any directory inside the project."`
	}) (*struct {
		Body struct {
			Named []string `json:"named"`
		}
	}, error) {
		sc, _, err := d.scope(in.Dir, "")
		if err != nil {
			return nil, err
		}
		out := &struct {
			Body struct {
				Named []string `json:"named"`
			}
		}{}
		out.Body.Named = []string{}
		for n := range sc.Project().Named {
			out.Body.Named = append(out.Body.Named, n)
		}
		sort.Strings(out.Body.Named)
		return out, nil
	})

	gat.Register(api, g, huma.Operation{
		OperationID: "graph", Method: http.MethodGet, Path: "/graph",
		Summary: "Nodes and edges induced by a query, for the graph view.",
	}, func(ctx context.Context, in *struct {
		Dir   string `query:"dir" doc:"Any directory inside the project."`
		Now   string `query:"now" doc:"Override the clock for @now."`
		Q     string `query:"q" required:"true" doc:"kgraph query DSL."`
		Depth int    `query:"depth" doc:"Hops of neighbourhood to include around the result." default:"1"`
		Limit int    `query:"limit" doc:"Maximum nodes to return." default:"250"`
	}) (*struct {
		Body struct {
			Nodes     []GraphNode `json:"nodes"`
			Edges     []GraphEdge `json:"edges"`
			Matched   int         `json:"matched"`
			Truncated bool        `json:"truncated"`
		}
	}, error) {
		sc, env, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		q, perr := ParseQuery(in.Q)
		if perr != nil {
			return nil, huma.Error422UnprocessableEntity(perr.Error())
		}
		if errs := ValidateQuery(q); len(errs) > 0 {
			return nil, huma.Error422UnprocessableEntity(errs[0].Error())
		}
		ids, eerr := sc.Project().Graph.Eval(q, env)
		if eerr != nil {
			return nil, huma.Error400BadRequest(eerr.Error())
		}
		out := &struct {
			Body struct {
				Nodes     []GraphNode `json:"nodes"`
				Edges     []GraphEdge `json:"edges"`
				Matched   int         `json:"matched"`
				Truncated bool        `json:"truncated"`
			}
		}{}
		out.Body.Matched = len(ids)
		out.Body.Nodes, out.Body.Edges, out.Body.Truncated =
			sc.Project().Graph.Induced(ids, in.Depth, in.Limit, env.Now)
		return out, nil
	})

	gat.Register(api, g, huma.Operation{
		OperationID: "node", Method: http.MethodGet, Path: "/node",
		Summary: "One node, rendered the way a prompt would see it, plus its edges.",
	}, func(ctx context.Context, in *struct {
		Dir string `query:"dir" doc:"Any directory inside the project."`
		Now string `query:"now" doc:"Override the clock for @now."`
		ID  string `query:"id" required:"true" doc:"Node id, or an alias."`
	}) (*struct {
		Body struct {
			ID       string      `json:"id"`
			Rendered string      `json:"rendered"`
			SemHash  string      `json:"sem_hash"`
			Holes    []string    `json:"holes,omitempty"`
			Edges    []GraphEdge `json:"edges"`
			UsedBy   []string    `json:"used_by,omitempty"`
		}
	}, error) {
		sc, env, err := d.scope(in.Dir, in.Now)
		if err != nil {
			return nil, err
		}
		gr := sc.Project().Graph
		id, rerr := gr.ResolveRef(in.ID, env.Now)
		if rerr != nil {
			return nil, huma.Error422UnprocessableEntity(rerr.Error())
		}
		if gr.Nodes[id] == nil {
			return nil, huma.Error404NotFound("no node " + in.ID)
		}
		out := &struct {
			Body struct {
				ID       string      `json:"id"`
				Rendered string      `json:"rendered"`
				SemHash  string      `json:"sem_hash"`
				Holes    []string    `json:"holes,omitempty"`
				Edges    []GraphEdge `json:"edges"`
				UsedBy   []string    `json:"used_by,omitempty"`
			}
		}{}
		out.Body.ID = id
		out.Body.Rendered = gr.Label(id)
		out.Body.SemHash = gr.SemHash[id]
		out.Body.Holes = gr.Taint(id, env.Now)
		for _, e := range gr.Incident(id) {
			out.Body.Edges = append(out.Body.Edges, GraphEdge{
				Src: e.Src, Dst: e.Dst, Type: string(e.Type)})
		}
		// Which STANDING QUERIES this fact is in — the question a reader actually
		// has. It used to be "which documents pin it", read from each spec's sealed
		// managed block; the answer is now live, because a standing query resolves
		// rather than remembering.
		if set, serr := ReadStanding(sc.Root, IndexDirOf(gr)); serr == nil {
			for _, st := range set {
				a, aerr := gr.Ask(st, env)
				if aerr != nil {
					continue
				}
				for _, row := range a.Rows {
					if row == id {
						out.Body.UsedBy = appendUnique(out.Body.UsedBy, st.Name)
						break
					}
				}
			}
		}
		return out, nil
	})

	return nil
}

func diagText(ds []Diag) string {
	var out string
	for _, d := range ds {
		out += d.String() + "\n"
	}
	if out == "" {
		return "the fragment does not parse"
	}
	return out
}

// Serve runs the daemon. One process, many projects, many branches.
func (d *Daemon) Serve(addr string) error {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("kgraph", "0.1.0"))
	g, err := gat.New()
	if err != nil {
		return fmt.Errorf("gat.New: %w", err)
	}
	if err := d.Register(api, g); err != nil {
		return err
	}
	if err := gat.RegisterHuma(api, g, "/api"); err != nil {
		return fmt.Errorf("RegisterHuma: %w", err)
	}
	if err := gat.RegisterGRPC(mux, g, "/api/grpc"); err != nil {
		return fmt.Errorf("RegisterGRPC: %w", err)
	}
	mountUI(mux)
	mountEvents(mux, d.Reg)

	// Wrapped unconditionally. The gate is per-peer, so loopback callers are
	// unaffected and a listener that is loopback today cannot become open by an
	// address change alone.
	tok, terr := DaemonToken()
	if terr != nil {
		return fmt.Errorf("cannot establish a daemon token, refusing to listen without one: %w", terr)
	}
	h := requireToken(mux, tok)
	if notice := exposureNotice(addr, tok); notice != "" {
		fmt.Fprint(os.Stderr, notice)
	}
	fmt.Fprintf(os.Stderr, "kgraph daemon on http://%s\n  UI       /\n"+
		"  REST     /projects /status /diff /variants /query /graph /node /render /attach /scan\n"+
		"  GraphQL  /api/graphql\n  OpenAPI  /openapi.json\n", addr)
	return http.ListenAndServe(addr, h)
}

// AnswerState is one standing query's headline: how many rows, and what moved.
type AnswerState struct {
	Name  string   `json:"name"`
	Rows  int      `json:"rows"`
	Drift []string `json:"drift,omitempty"`
}

// answers resolves a scope's standing queries, optionally just one by name.
//
// Silent on a missing or unreadable store: a corpus nobody has asked anything of
// is the normal starting state, and a daemon that errored on it would refuse to
// report a graph that is otherwise fine.
func (d *Daemon) answers(sc *Scope, env Env, only string) []*Answer {
	set, err := ReadStanding(sc.Root, IndexDirOf(sc.Project().Graph))
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(set))
	for n := range set {
		if only != "" && n != only {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	var out []*Answer
	for _, n := range names {
		a, aerr := sc.Project().Graph.Ask(set[n], env)
		if aerr != nil {
			continue
		}
		out = append(out, a)
	}
	return out
}

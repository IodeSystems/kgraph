package kgraph

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/iodesystems/raglit/client"
)

// What raglit's rulings mean to the graph.
//
// `checkSameDocument` already catches two sources registered against ONE file,
// and its own reasoning says why it stops there: "the `doc:` path is not a
// similarity score." Exact path identity is the only thing it can be sure of
// without guessing, and guessing at document identity from a similarity number
// is how a corpus acquires false merges.
//
// raglit closes exactly that gap, and closes it with a RULING rather than a
// score. `raglit marks` puts an aligned pair in front of a person with the
// evidence attached — how much text is shared, where, and which numbers differ —
// and records what they decide in relations.jsonl. Two outcomes matter here and
// they are not the same finding:
//
//	COPY     two files, one instrument, no substantive difference. The graph is
//	         citing one thing under two names. Usually fine; worth saying once,
//	         because a reader comparing two facts cannot tell that their sources
//	         are the same document.
//	VERSION  two files, one instrument, filed or corrected differently. If the
//	         ruling also says which side GOVERNS, then a fact resting on the
//	         other side rests on superseded evidence — and that is the failure
//	         this whole tool exists to catch.
//
// ASKED FOR, never read off disk. An earlier version of this file parsed
// raglit's relations.jsonl directly; raglit then moved rulings into a database
// projected from an audit trail, and this went on reading a file nobody wrote
// any more — no error, just stale answers. Parsing another tool's storage makes
// every storage change a silent break in a program nobody edited.
//
// So the contract is raglit's HTTP API, via its client package. raglit may
// reorganise its storage whenever it likes.
//
// Unreachable is NOT empty. When no daemon is running, Relations stays nil,
// which means NOT LOADED and is silent — the same distinction SourceDrift makes.
// Reporting "no duplicates found" because a daemon was down would be the exact
// failure this arrangement exists to prevent.

// RelationKind mirrors raglit's MarkKind, as a kgraph-side type: the wire values
// are raglit's, and this keeps the graph's vocabulary its own.
type RelationKind string

const (
	RelationCopy      RelationKind = "copy"
	RelationVersion   RelationKind = "version"
	RelationUnrelated RelationKind = "unrelated"
)

// DocRelation is one ruling, as raglit wrote it.
type DocRelation struct {
	A          string       `json:"a"`
	B          string       `json:"b"`
	Kind       RelationKind `json:"kind"`
	Supersedes string       `json:"supersedes,omitempty"`
	Note       string       `json:"note,omitempty"`
	By         string       `json:"by,omitempty"`
	At         string       `json:"at,omitempty"`
}

// RelationSet is the rulings for a corpus, keyed by normalized document pair.
type RelationSet map[string]DocRelation

func relKey(a, b string) string {
	if b < a {
		a, b = b, a
	}
	return a + "\x00" + b
}

// Of returns the ruling on a pair of document paths, in either order.
func (s RelationSet) Of(a, b string) (DocRelation, bool) {
	r, ok := s[relKey(a, b)]
	return r, ok
}

// FetchRelations asks raglit for this corpus's rulings.
//
// project is the directory holding raglit's judgement store — the same directory
// the facts live in. Paths come back absolute (raglit indexes absolute paths)
// and are relativized here, because a `doc:` in this corpus is root-relative and
// comparing the two unnormalized would match nothing, which looks exactly like
// "no duplicates found".
//
// Returns ErrRaglitUnavailable when raglit cannot be reached, so a caller can
// tell "nothing ruled" from "could not ask".
func FetchRelations(ctx context.Context, project string) (RelationSet, error) {
	c := client.New("")
	rels, err := c.Relations(ctx, project, "")
	if err != nil {
		if errors.Is(err, client.ErrUnavailable) {
			return nil, fmt.Errorf("%w: %v", ErrRaglitUnavailable, err)
		}
		return nil, err
	}
	out := RelationSet{}
	for _, r := range rels {
		dr := DocRelation{
			A: relToRoot(project, r.A), B: relToRoot(project, r.B),
			Kind: RelationKind(r.Kind), Supersedes: relToRoot(project, r.Supersedes),
			Note: r.Note, By: r.By, At: r.At,
		}
		if dr.A == "" || dr.B == "" {
			continue
		}
		out[relKey(dr.A, dr.B)] = dr
	}
	return out, nil
}

// ErrRaglitUnavailable distinguishes "could not ask" from "nothing ruled".
var ErrRaglitUnavailable = errors.New("raglit unavailable")

// relToRoot makes an absolute raglit path relative to the project directory, and
// leaves anything already relative alone. A path outside the tree is kept as-is
// rather than turned into a ../.. chain that would match no source.
func relToRoot(base, p string) string {
	if p == "" || !filepath.IsAbs(p) {
		return p
	}
	rel, err := filepath.Rel(base, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return filepath.ToSlash(rel)
}

// checkRelations reports what raglit's rulings mean for the sources in this
// graph.
//
// Silent when Relations is nil, which means NOT LOADED — not "nothing ruled".
// The distinction is the same one SourceDrift makes and matters for the same
// reason: an unloaded set must never read as reassurance.
func (g *Graph) checkRelations() []Diag {
	// The nil/empty distinction the comment above draws is enforced by the
	// CALLER — `ScanFactsIn` only sets `Relations` and calls this when raglit
	// actually answered — so testing it again here read as belt-and-braces and
	// was neither: both branches returned the same thing.
	if len(g.Relations) == 0 {
		return nil
	}
	// Sources by the document they cite, so a ruling about two files can be
	// turned into a statement about the ids a reader actually sees.
	byDoc := map[string][]string{}
	for id, n := range g.Nodes {
		if n.Kind != KSource || n.Source == nil || n.Source.DocPath == "" {
			continue
		}
		byDoc[n.Source.DocPath] = append(byDoc[n.Source.DocPath], id)
	}
	for d := range byDoc {
		sort.Strings(byDoc[d])
	}

	var out []Diag
	seen := map[string]bool{}
	for _, r := range g.Relations {
		if r.Kind == RelationUnrelated {
			continue
		}
		aIDs, bIDs := byDoc[r.A], byDoc[r.B]
		// A ruling about documents this graph does not cite is raglit's business,
		// not the graph's.
		if len(aIDs) == 0 || len(bIDs) == 0 {
			continue
		}
		key := relKey(r.A, r.B)
		if seen[key] {
			continue
		}
		seen[key] = true

		first := aIDs[0]
		node := g.Nodes[first]

		switch r.Kind {
		case RelationVersion:
			if r.Supersedes == "" {
				out = append(out, Diag{File: node.File, Line: node.Line, Severity: SevWarn, Msg: fmt.Sprintf(
					"%s and %s are two VERSIONS of one instrument (raglit), with no ruling on which governs — "+
						"a fact citing one is not supported by the other; record the order with `raglit mark ... --supersedes`",
					strings.Join(aIDs, ", "), strings.Join(bIDs, ", "))})
				continue
			}
			// The superseded side is the one that is NOT named as governing.
			staleDoc, staleIDs := r.A, aIDs
			liveDoc, liveIDs := r.B, bIDs
			if r.Supersedes == r.A {
				staleDoc, staleIDs, liveDoc, liveIDs = r.B, bIDs, r.A, aIDs
			}
			out = append(out, Diag{File: node.File, Line: node.Line, Severity: SevWarn, Msg: fmt.Sprintf(
				"%s cites %s, which raglit records as SUPERSEDED by %s (cited as %s) — "+
					"every fact resting on it rests on a version that no longer governs",
				strings.Join(staleIDs, ", "), staleDoc, liveDoc, strings.Join(liveIDs, ", "))})

		case RelationCopy:
			// Two ids for one instrument is not wrong — a filing and its exhibit
			// legitimately both exist — but a reader comparing two facts cannot
			// see that their sources are the same document, and two "independent"
			// sources that are one document is a corroboration that is not there.
			out = append(out, Diag{File: node.File, Line: node.Line, Severity: SevWarn, Msg: fmt.Sprintf(
				"%s and %s are the SAME instrument under two files (raglit: copy) — "+
					"they do not corroborate each other; consider one id with an alias for the other",
				strings.Join(aIDs, ", "), strings.Join(bIDs, ", "))})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

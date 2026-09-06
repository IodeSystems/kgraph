package kgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// DECLARED QUERIES — what was asked, and the answer it resolves to.
//
// This file is what survived `spec.go`. `*.kgraph.md` retired 2026-09-01: a
// spec stripped of prompt, outputs and managed block was a named query with a
// purpose, which is a second way to say what `standing.yaml` says, and two
// idioms for one thing is the failure this format refuses everywhere else.
// `Pin` and `Resolve` outlived it unchanged, because they were never about
// documents — they are about a set somebody committed to.

// Pin is the resolved set a document's prose committed to. Members alone are not
// enough: the text says "three blockers" and "the earliest is X", so count,
// order and grouping are pinned too.
type Pin struct {
	SetHash string   `yaml:"set_hash"`
	Count   int      `yaml:"count"`
	Sort    []string `yaml:"sort,omitempty"`
	Nodes   []string `yaml:"nodes"` // "id@semhash", in resolved order
	// Unresolved is the open questions this set rests on. A section can be fully
	// attested and still not settled; pinning the holes means a question being
	// answered registers as a change even when no member moved.
	Unresolved []string `yaml:"unresolved,omitempty"`
	// Cites is the sources this set's rows cite, "id@semhash". A source is not a
	// row, so it never appeared in Nodes — yet `attach` resolves its class and its
	// document path INTO the artifact. Reorganising a corpus therefore changed
	// what the document says while every pinned row still matched, and the
	// document reported `fresh`. Same reasoning as Unresolved: something the
	// section rests on that is not a member of it.
	Cites []string `yaml:"cites,omitempty"`
}

// ParseSpec reads a spec file. It never treats the managed region as authored
// input, and it verifies that region's self-hash rather than trusting it.
func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}

// ── resolution ─────────────────────────────────────────────────────────

// Resolve evaluates every query in the spec and pins the realized set.
func (g *Graph) Resolve(s DeclaredSet, env Env) (map[string]*Pin, error) {
	out := map[string]*Pin{}
	for _, sq := range s.Queries {
		q, err := ParseQuery(sq.Text)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sq.Name, err)
		}
		ids, err := g.Eval(q, env)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sq.Name, err)
		}
		p := &Pin{Count: len(ids)}
		if pat, ok := q.(Pattern); ok {
			for _, k := range pat.Sort {
				p.Sort = append(p.Sort, k.Key)
			}
		}
		if len(p.Sort) == 0 {
			p.Sort = []string{"id"}
		}
		for _, id := range ids {
			p.Nodes = append(p.Nodes, id+"@"+strings.TrimPrefix(g.SemHash[id], "sha256:"))
		}
		p.Unresolved = g.TaintedSet(ids, env.Now)
		p.Cites = g.citedSources(ids)
		joined := append([]string{}, p.Nodes...)
		joined = append(joined, p.Unresolved...)
		joined = append(joined, p.Cites...)
		p.SetHash = hashString(strings.Join(joined, "\n"))
		out[sq.Name] = p
	}
	return out, nil
}

// ── state ──────────────────────────────────────────────────────────────

func dedupe(in []string) []string {
	var out []string
	for i, s := range in {
		if i == 0 || in[i-1] != s {
			out = append(out, s)
		}
	}
	return out
}

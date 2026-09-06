package kgraph

import "testing"

// A DOCUMENT THAT IS NOT A PAGE IMAGE IS STILL EVIDENCE.
//
// `/api/doc` rasterised a PDF and handed everything else back with ServeFile,
// and the page put all of it into an <img>. Measured on a live queue, 337 of
// 926 held documents — every .md, .docx, .txt, .eml and .csv, 36% — rendered as
// a broken image with no evidence on screen at all. A .docx went over the wire
// as a zip container and a .doc as OLE.
func TestSourceKindSeparatesWhatRendersFromWhatMustBeRead(t *testing.T) {
	for _, c := range []struct{ rel, want string }{
		{"a/scan.pdf", "pdf"},
		{"a/photo.jpg", "image"},
		{"a/note.md", "text"},
		{"a/mail.txt", "text"},
		{"a/weather.csv", "text"},
		{"a/brief.docx", "binary"},
		{"a/old.doc", "binary"},
	} {
		if got := sourceKind(c.rel); got != c.want {
			t.Errorf("sourceKind(%q) = %q, want %q", c.rel, got, c.want)
		}
	}
	// The two that matter: neither renders, so neither may reach an <img>.
	for _, rel := range []string{"a/brief.docx", "a/note.md"} {
		if k := sourceKind(rel); k == "pdf" || k == "image" {
			t.Errorf("%s classified %q — it would be put into an <img>", rel, k)
		}
	}
}

// EXISTING CITATIONS ARE CARRIED FORWARD EXACTLY AS AUTHORED.
//
// A bare id and a `{id, because}` mapping parse to the same edge, so rewriting
// the untouched ones into the other form would move every one of their
// sem_hashes and flag every document that renders the fact — for a change
// nobody made.
func TestMergeCitationsDoesNotRewriteWhatItDidNotTouch(t *testing.T) {
	existing := []any{
		"s-old-bare",
		map[string]any{"id": "s-old-mapped", "because": "already explained"},
	}
	list, added := mergeCitations(existing, []Cite{
		{ID: "s-bn-1979-license", Because: "  grants the railroad-grade access  "},
		{ID: "s-no-reason"},
	})
	if len(added) != 2 {
		t.Fatalf("added = %v, want both", added)
	}
	if s, ok := list[0].(string); !ok || s != "s-old-bare" {
		t.Errorf("a bare id was rewritten: %#v", list[0])
	}
	if m, ok := list[1].(map[string]any); !ok || m["because"] != "already explained" {
		t.Errorf("an authored because was disturbed: %#v", list[1])
	}
	// New entries are mapping form, trimmed, and omit an empty because rather
	// than writing one — an empty reason is not a reason.
	m := list[2].(map[string]any)
	if m["id"] != "s-bn-1979-license" || m["because"] != "grants the railroad-grade access" {
		t.Errorf("the new edge lost its id or its because: %#v", m)
	}
	if m2 := list[3].(map[string]any); m2["because"] != nil {
		t.Errorf("an empty because was written: %#v", m2)
	}
}

// Adding an edge twice says nothing new, in either authored form.
func TestMergeCitationsIgnoresOnesAlreadyCited(t *testing.T) {
	existing := []any{"s-bare", map[string]any{"id": "s-mapped"}}
	list, added := mergeCitations(existing, []Cite{
		{ID: "s-bare", Because: "different words, same edge"},
		{ID: "s-mapped"},
		{ID: ""},
	})
	if len(added) != 0 {
		t.Errorf("added = %v, want none", added)
	}
	if len(list) != 2 {
		t.Errorf("the list grew to %d: %#v", len(list), list)
	}
}

// A fact whose `attested_by` was authored as a lone scalar — which is legal, and
// most of the corpus — must become a two-element list, not be overwritten.
func TestMergeCitationsPromotesALoneScalar(t *testing.T) {
	list, added := mergeCitations("s-only", []Cite{{ID: "s-second", Because: "the other half"}})
	if len(added) != 1 || len(list) != 2 {
		t.Fatalf("list=%#v added=%v", list, added)
	}
	if list[0] != any("s-only") {
		t.Errorf("the original citation was lost: %#v", list[0])
	}
}

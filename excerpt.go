package kgraph

import (
	"fmt"
	"strings"
)

// Does the verdict still stand over the words it was made about?
//
// An attestation is one person's judgement on one (fact, source) pair, and it
// records WHAT THEY READ — see Attestation.Excerpt. This is the check that pins
// it, and it exists because a verdict could outlive its passage two ways and
// nothing could tell:
//
//   - THE TRANSCRIPTION MOVED. A re-OCR, a better model, a re-scan at a
//     different DPI. `HashDoc` digests the ORIGINAL FILE's bytes, so the PDF is
//     unchanged and source drift does not fire — while the words anybody
//     actually reads and quotes may be entirely different. This is the likeliest
//     way a passage moves and it was invisible to every existing check.
//   - THE FACT MOVED. An attestation is keyed on (fact, source) and survives an
//     edit to either, so a quotation reworded after attestation carries a verdict
//     nobody gave about those words.
//
// Neither is a refutation and this says so: the finding is that the record and
// the corpus disagree, and a person decides which is right. That is the same
// posture `CheckQuotes` takes — this records nothing and withdraws nothing.

// QuoteIn reports whether a document currently contains a passage.
//
// (present, readable). READABLE IS THE SECOND ANSWER because it is a different
// state: a document nobody can read is not a document that disagrees, and a
// caller that collapses them reports every un-OCR'd scan as a miscitation.
//
// EXPORTED SO NOBODY REIMPLEMENTS THE FOLDING. Whether a quotation is present is
// not a substring test — pleading line numbers interleave with the text, a
// transcription wraps mid-phrase, curly quotes and whitespace differ — and the
// rules for all of that live in this file. A second copy is the drift this
// package's consumers are told to avoid everywhere else.
func QuoteIn(root, rel, quote string) (present, readable bool) {
	if strings.TrimSpace(quote) == "" || strings.TrimSpace(rel) == "" {
		return false, false
	}
	raw := docText(root, rel)
	if raw == "" {
		return false, false
	}
	body := foldText(stripPleadingLineNumbers(raw))
	return strings.Contains(body, foldText(quote)), true
}

// ExcerptCheck is one attestation measured against the corpus as it stands.
type ExcerptCheck struct {
	Fact   string `json:"fact"`
	Source string `json:"source"`
	Doc    string `json:"doc,omitempty"`
	// Excerpt is what the attestation recorded, verbatim.
	Excerpt string `json:"excerpt"`
	// State is `intact`, `document-moved`, `fact-moved`, or `unreadable`.
	//
	// FOUR, not two, for the reason `not-held`/`not-looked`/`does-not-exist` are
	// three: a document nobody can read is not a document that disagrees, and a
	// fact that was reworded is a different problem from a transcription that
	// was replaced. Collapsing any of them sends somebody to fix the wrong end.
	State string `json:"state"`
	Why   string `json:"why"`
}

const (
	ExcerptIntact     = "intact"
	ExcerptDocMoved   = "document-moved"
	ExcerptFactMoved  = "fact-moved"
	ExcerptUnreadable = "unreadable"
)

// CheckAttestedExcerpts measures every recorded excerpt against the corpus.
//
// Ordered by fact then source, because a report that reorders itself between two
// runs over an unchanged corpus is one nobody can diff.
func (g *Graph) CheckAttestedExcerpts(root string, set AttestSet) []ExcerptCheck {
	cache := map[string]foldedDoc{}
	read := func(rel string) foldedDoc {
		if _, ok := cache[rel]; !ok {
			raw := docText(root, rel)
			if raw == "" {
				cache[rel] = foldedDoc{}
			} else {
				t := stripPleadingLineNumbers(raw)
				f, offs := foldTextIdx(t)
				cache[rel] = foldedDoc{text: t, body: f, offs: offs}
			}
		}
		return cache[rel]
	}

	var out []ExcerptCheck
	for _, a := range set.sorted() {
		if strings.TrimSpace(a.Excerpt) == "" {
			continue
		}
		c := ExcerptCheck{Fact: a.Fact, Source: a.Source, Excerpt: a.Excerpt}

		// THE FACT FIRST, because it needs no document and it is the cheaper
		// mistake to make. A reworded quotation is an edit somebody made on
		// purpose; a moved transcription is something that happened to them.
		if n, ok := g.Lookup(a.Fact); ok {
			if now := ExcerptOf(n.Body); now != "" && HashExcerpt(now) != HashExcerpt(a.Excerpt) {
				c.State = ExcerptFactMoved
				c.Why = fmt.Sprintf("the fact quoted %q when it was attested and quotes %q now — "+
					"the verdict is about words this fact no longer uses", a.Excerpt, now)
				out = append(out, c)
				continue
			}
		}

		sn, ok := g.Lookup(a.Source)
		if !ok || sn.Source == nil {
			continue
		}
		rel, err := DocPathOf(root, sn)
		if err != nil || rel == "" {
			continue
		}
		c.Doc = rel
		doc := read(rel)
		if doc.body == "" {
			// UNREADABLE IS NOT ABSENT. Silence about a document nobody can read
			// is not evidence against the attestation, and reporting it as a
			// mismatch would send somebody to re-verify a verdict that is fine.
			c.State = ExcerptUnreadable
			c.Why = "nothing can read this document now, so the excerpt cannot be checked — " +
				"which is not the same as the words having changed"
			out = append(out, c)
			continue
		}
		if strings.Contains(doc.body, foldText(a.Excerpt)) {
			c.State = ExcerptIntact
			c.Why = "the words the verdict was given over are still in the document"
			out = append(out, c)
			continue
		}
		c.State = ExcerptDocMoved
		c.Why = "the document no longer contains the words this verdict was given over. Its BYTES " +
			"may be untouched — a re-reading of the same scan changes the text and not the file — " +
			"so nothing else reports this"
		out = append(out, c)
	}
	return out
}

// Diags renders the checks that want a person, and never the intact ones. A
// report that lists what is fine teaches people to skim it.
func ExcerptDiags(g *Graph, checks []ExcerptCheck) []Diag {
	var out []Diag
	for _, c := range checks {
		if c.State == ExcerptIntact {
			continue
		}
		sev := SevWarn
		if c.State == ExcerptDocMoved || c.State == ExcerptFactMoved {
			sev = SevError
		}
		var file string
		var line int
		if n, ok := g.Lookup(c.Fact); ok {
			file, line = n.File, n.Line
		}
		out = append(out, Diag{File: file, Line: line, Severity: sev,
			Check: CheckAttestedExcerpt, Key: findingKey(g, CheckAttestedExcerpt, c.Fact, c.Source),
			Subjects: []string{c.Fact, c.Source},
			Msg: fmt.Sprintf("%s attested to %s: %s. Re-read it and attest again, or correct "+
				"the citation", c.Fact, c.Source, c.Why)})
	}
	return out
}

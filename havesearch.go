package kgraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iodesystems/raglit/client"
)

// Asking raglit for document content, instead of grepping sidecars.
//
// The content tier used to read a `.raglit-transcription.md` sitting beside each
// document. That made the sidecar the corpus: a document without one was
// unsearchable however much text it had, so nine Word declarations and every
// court recording were declared, citable, and unverifiable — and deleting the
// sidecars would have blinded the whole evidence layer while leaving every
// instrument in place.
//
// raglit already holds the text of all of them. It extracts .docx natively,
// OCRs scans, and transcribes audio, so the question "which documents use these
// words" is one it can already answer for the entire corpus. Asking it is also
// the arrangement `relations.go` already argues for: the contract is raglit's
// HTTP API, so raglit may reorganise its storage whenever it likes, and kgraph
// does not go on reading a file nobody writes any more.
//
// UNREACHABLE IS AN ERROR HERE, not an empty result, and this is the one place
// that rule cannot bend. Everything above this comment exists to tell `not-held`
// from `not-looked`; a stopped daemon reported as "nothing bears on that
// passage" tells a person to stop looking for evidence that is sitting in the
// corpus. `FetchRelations` makes the same distinction for the same reason.

// raglitIndexName returns the daemon index for a corpus, or "" when it has none.
//
// The name is raglit's, and it is namespaced: a project-scoped index is
// "<project>__<index>". Read from the corpus's own `.raglit/config.json` rather
// than composed from the directory name, because the project name is a raglit
// setting and a corpus may be indexed under a name its folder does not carry.
//
// "" is a real answer and means THIS CORPUS HAS NO INDEX — a temp directory in a
// test, or a tree nobody has ingested. Those fall back to reading whatever text
// is on disk, because there is no daemon to be down.
func raglitIndexName(root string) string {
	b, err := readRaglitConfig(root)
	if err != nil {
		return ""
	}
	var cfg struct {
		Project      string `json:"project"`
		DefaultIndex string `json:"default_index"`
	}
	if json.Unmarshal(b, &cfg) != nil || cfg.Project == "" {
		return ""
	}
	idx := cfg.DefaultIndex
	if idx == "" {
		idx = "default"
	}
	return cfg.Project + "__" + idx
}

// readRaglitConfig finds the corpus's raglit configuration.
//
// AT THE ROOT, OR IN A PROJECT UNDER IT — and looking only at the root is why
// every quotation in a live corpus reported `unreadable`. raglit is configured
// per PROJECT: the config sits at `<matter>/.raglit/config.json` and names the
// project, because that is the directory raglit indexes. kgraph is run from the
// corpus root, found no `.raglit` there, returned an empty index name, and every
// text lookup short-circuited — silently, because "no raglit" is a legitimate
// state that must degrade to "not loaded" rather than to an error.
//
// 192 of 303 quotations read `unreadable` under that, which looks exactly like a
// corpus whose evidence has never been transcribed. The transcriptions existed;
// nothing was asking for them.
//
// One level deep, and no deeper: `<root>/<project>/.raglit` is where a corpus of
// matters puts them, and a full walk of a corpus holding a gigabyte of evidence
// to find a config file is not worth the alternative.
func readRaglitConfig(root string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(root, ".raglit", "config.json"))
	if err == nil {
		return b, nil
	}
	for _, dir := range []string{"projects", "."} {
		entries, rerr := os.ReadDir(filepath.Join(root, dir))
		if rerr != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if b, rerr := os.ReadFile(filepath.Join(root, dir, e.Name(), ".raglit", "config.json")); rerr == nil {
				return b, nil
			}
		}
	}
	return nil, err
}

// contentHits asks raglit which documents use the query's words.
//
// Two queries, because the tiers they feed are not equally trustworthy and
// `viaRank` orders them: an identifier in a body says the document names the
// instrument (`text`), while shared vocabulary says only that it discusses the
// same subject (`terms`). Collapsing them into one score would lose the
// distinction the ranking is built on.
//
// Returns ErrRaglitUnavailable when the daemon cannot be reached.
func contentHits(root, index, query string, want map[string]bool, terms []string, limit int) (map[string]searchHit, error) {
	out := map[string]searchHit{}
	if index == "" {
		return out, nil
	}
	c := client.New("")
	ctx := context.Background()
	ask := func(q, via string) error {
		if strings.TrimSpace(q) == "" {
			return nil
		}
		hits, err := c.Search(ctx, index, q, limit)
		if err != nil {
			if errors.Is(err, client.ErrUnavailable) {
				return fmt.Errorf("%w: %v", ErrRaglitUnavailable, err)
			}
			return err
		}
		for _, h := range hits {
			rel := relToRoot(root, h.Doc)
			if rel == "" || strings.HasPrefix(rel, "..") {
				continue
			}
			// COUNTED, not carried over from the query. Every hit of one ask
			// shares its query, so using the query's length gave every document
			// the same score, every comparison tied, and `Have` fell back to
			// sorting by path — which put an alphabetically early declaration
			// above the transcript that actually contained the words. Counting
			// the query's terms present in the snippet restores raglit's
			// ordering AND keeps the number the reader is shown honest, since
			// `terms (N words)` claims to say how much vocabulary is shared.
			n := 0
			low := strings.ToLower(h.Snippet)
			for _, t := range strings.Fields(strings.ToLower(q)) {
				if strings.Contains(low, t) {
					n++
				}
			}
			// The stronger tier wins, and `text` outranks `terms`.
			if prev, ok := out[rel]; ok && (viaRank[prev.via] < viaRank[via] ||
				(viaRank[prev.via] == viaRank[via] && prev.terms >= n)) {
				continue
			}
			out[rel] = searchHit{via: via, excerpt: excerpt(h.Snippet), terms: n,
				caveat: h.Caveat, trust: h.Trust}
		}
		return nil
	}
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	if err := ask(strings.Join(ids, " "), "text"); err != nil {
		return nil, err
	}
	// THE WHOLE PASSAGE, not the extracted terms. `contentTerms` exists to keep a
	// substring scan from matching on "the" — a filter this tier does not need,
	// because BM25 already weights a rare word above a common one. Handing it the
	// filtered list instead threw away the phrase and the ranking with it: the
	// query that finds a hearing transcript at 11.61 against 4.0 for the next
	// document returned neither once it had been reduced to two surviving words.
	q := query
	if strings.TrimSpace(q) == "" {
		q = strings.Join(terms, " ")
	}
	if err := ask(q, "terms"); err != nil {
		return nil, err
	}
	return out, nil
}

// searchHit is one document raglit matched, in this package's vocabulary.
type searchHit struct {
	via     string
	excerpt string
	terms   int
	// What raglit says about the TEXT this excerpt was taken from. Kept beside
	// the excerpt because they travel together or the excerpt lies by omission.
	caveat string
	trust  *client.HitTrust
}

// docText returns a document's readable text, from raglit.
//
// The single place the rest of kgraph asks "what does this document say". It
// replaced a scatter of readers that each opened a `.raglit-transcription.md`
// beside the file, which is what made the sidecar the corpus: a document raglit
// could read perfectly well — a .docx, a recording — had no readable text at all
// as far as kgraph was concerned, because nobody had written a file next to it.
//
// Falls back to the sidecar ONLY when the corpus has no raglit index, which is a
// test's temp tree. Returns "" when raglit cannot answer; callers here are
// rendering or counting rather than deciding possession, and `Have` is the one
// that must turn silence into an error.
func docText(root, rel string) string {
	if idx := raglitIndexName(root); idx != "" {
		abs, err := filepath.Abs(filepath.Join(root, rel))
		if err == nil {
			if doc, derr := client.New("").Document(context.Background(), idx, abs); derr == nil {
				if doc.Text != "" {
					return doc.Text
				}
				var b strings.Builder
				for _, p := range doc.Pages {
					b.WriteString(p.Text)
					b.WriteString("\n")
				}
				return strings.TrimSpace(b.String())
			}
		}
		// FALL THROUGH, never return empty here. raglit not holding a document is
		// not the same as the document having no text: a corpus indexes what its
		// config's `include` patterns cover, and everything else — case law kept as
		// markdown, a transcription sidecar written by hand — is still readable
		// from disk.
		//
		// This returned "" and cost 76 quotations their verification the moment the
		// index name started resolving. Before that, the name was always empty, the
		// branch was never taken, and every document fell through to the read below
		// — so fixing the name silently disabled the fallback that had been doing
		// all the work.
	}
	tr, _ := companions(root, rel)
	if tr == "" {
		if !isTextDoc(rel) {
			return ""
		}
		tr = rel
	}
	b, err := os.ReadFile(filepath.Join(root, tr))
	if err != nil {
		return ""
	}
	return string(b)
}

// docsWithText is every document raglit can READ, as root-relative paths.
//
// ONE call, not one per file. `extract`'s backlog asks "is this readable" for
// every document in the corpus, and asking raglit per document turned a listing
// into hundreds of round trips.
//
// nil means "could not ask" and is not the same as an empty corpus; callers here
// fall back to what is on disk rather than declaring the whole backlog unread.
//
// IN THE INDEX IS NOT THE SAME AS HAS TEXT, and this counted the first while its
// name promised the second. A document with zero fragments is indexed and was
// never read — an un-OCR'd scan, an uncaptioned image — and marking it READY
// puts it in front of somebody to classify with nothing to classify from.
// Measured on a real corpus: 359 of 845 undeclared documents had no text
// anywhere, on disk or in the index. That is `not-looked` wearing a different
// coat, and the next step for it is INGEST, not a ruling.
//
// (Until the raglit client was fixed this returned nil on every call, because
// its decode could not have succeeded — so the fallback below ran for every
// corpus and the index was never consulted at all.)
func docsWithText(root string) map[string]bool {
	idx := raglitIndexName(root)
	if idx == "" {
		return nil
	}
	docs, err := client.New("").Documents(context.Background(), idx)
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(docs))
	for _, d := range docs {
		if d.Fragments == 0 {
			continue
		}
		if rel := relToRoot(root, d.Path); rel != "" && !strings.HasPrefix(rel, "..") {
			out[rel] = true
		}
	}
	return out
}

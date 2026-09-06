package kgraph

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/iodesystems/raglit/client"
)

// The attestation workbench.
//
// `kg attest --todo` lists 407 citations on the fence-dispute corpus. A list is not a
// tool: working it means, for each one, finding the document, finding the place
// in it, deciding, and recording — and doing that four hundred times from a
// shell is how a checklist stops being worked.
//
// So this is oidio's verify in the other medium. One citation on screen at a
// time, the document beside it, drag a box over the proof, one key to rule. It
// serves on the LAN because the browser is not on the dev box.
//
// Deliberately NOT part of `kg daemon`: the daemon is a singleton across every
// project with a token gate, and this is a focused per-index session a person
// opens, works, and closes. Same reason `oidio verify` is its own command.

//go:embed verify.html
var verifyHTML []byte

// VerifyItem is one citation to rule on, with everything the page needs to show
// it without a second round trip.
type VerifyItem struct {
	Fact     string `json:"fact"`
	FactBody string `json:"factBody"`
	FactKind string `json:"factKind"`
	Why      string `json:"why,omitempty"`
	Source   string `json:"source"`
	SrcBody  string `json:"srcBody"`
	Class    string `json:"class,omitempty"`
	Doc      string `json:"doc,omitempty"`
	Pages    int    `json:"pages,omitempty"`
	// Kind is how the page must SHOW this document: "pdf" and "image" render,
	// "text" and "binary" have to be read as text.
	//
	// Decided here rather than from the extension in the browser, so the two
	// halves cannot disagree about what a `.docx` is. They did: the page put
	// every document into an <img>, so 337 of 926 held documents — every .md,
	// .docx, .txt, .eml and .csv, 36% of the queue — showed a broken image and
	// no evidence at all.
	Kind string `json:"kind,omitempty"`
	// Held is false when the source has no `doc:` at all. Those are still worth
	// ruling on — `unsupported` is exactly the verdict for a citation to a
	// document nobody has — so they are listed rather than hidden.
	Held    bool     `json:"held"`
	Prior   string   `json:"prior,omitempty"` // an existing verdict, if any
	PriorBy string   `json:"priorBy,omitempty"`
	Region  *Region  `json:"region,omitempty"`
	Related []string `json:"related,omitempty"` // other facts on this same source
	// UsedBy names the specs that actually render this fact. A citation no
	// document relies on can be checked later; one three documents rest on is
	// load-bearing now.
	UsedBy []string `json:"usedBy,omitempty"`
	// LastPage is where a person last found something in THIS document, from an
	// earlier verdict on any fact citing it.
	//
	// Consecutive claims usually share a source — 23 facts rest on the answer — and
	// for a scan with no searchable text the page hint has nothing to offer, so
	// every one of them opened at page 1 and had to be re-navigated by hand. The
	// document has not changed; only the claim has.
	LastPage int `json:"lastPage,omitempty"`
}

// ServeVerify runs the workbench until interrupted.
func ServeVerify(root, index, addr, by string, ref StoreRef) error {
	// REFUSED AT THE DOOR. The workbench is the surface a PERSON rules at, and it
	// stamps this one signature on every verdict of the session. A machine
	// identity here is not a lesser signature, it is a discarded one: every
	// verdict written comes straight back through `PendingHuman`, which re-queues
	// anything `MachineAttested`, so a person works the list and the list does not
	// shrink. `unsupported` is refused outright and at least says why.
	//
	// This defaulted to `LastSigner`, which is whoever wrote the last line — so
	// one agent run was enough to leave the human workbench signing as
	// `agent-vision`.
	if IsMachineIdentity(by) {
		return fmt.Errorf("the workbench signs every verdict as %q, which is a machine "+
			"identity: those verdicts are re-queued as pending rather than recorded, so the "+
			"session would discard itself. Start it with `--by <person>`", by)
	}
	mux := http.NewServeMux()
	dir := ""

	// ONE store for reads and writes. The queue is built from a fold of the log
	// and `Supported by` appends to that same log; opening a different one for
	// each would let the workbench show a graph it cannot write to.
	st, serr := OpenStoreRef(ref)
	if serr != nil {
		return serr
	}
	if st != nil {
		defer st.Close()
	}

	load := func() (*Project, AttestSet, error) {
		p, _, err := LoadInWith(root, index, st)
		if err != nil {
			return nil, nil, err
		}
		dir = IndexDirOf(p.Graph)
		att, err := ReadAttestations(root, dir)
		return p, att, err
	}
	if _, _, err := load(); err != nil {
		return err
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(verifyHTML)
	})

	// The queue. Rebuilt per request so a verdict recorded a moment ago is gone
	// from it, and so editing a fact file mid-session is picked up.
	mux.HandleFunc("/api/pending", func(w http.ResponseWriter, r *http.Request) {
		p, att, err := load()
		if err != nil {
			httpErr(w, err)
			return
		}
		bySource := map[string][]string{}
		for _, e := range p.Graph.Edges {
			if e.Type == EAttests {
				bySource[e.Src] = append(bySource[e.Src], e.Dst)
			}
		}
		used := p.FactsUsedByDeclarations(Env{Now: r.URL.Query().Get("now")})
		// Where a person last landed in each document, so a sibling claim starts
		// there instead of at page 1.
		lastPage := map[string]int{}
		for _, at := range att {
			if at.Region.Page > 0 {
				lastPage[at.Source] = at.Region.Page
			}
		}
		onlyUsed := r.URL.Query().Get("used") == "1"
		var out []VerifyItem
		for _, a := range p.Graph.PendingHuman() {
			if onlyUsed && len(used[a.Fact]) == 0 {
				continue
			}
			fn := p.Graph.Nodes[a.Fact]
			sn := p.Graph.Nodes[a.Source]
			if fn == nil || sn == nil {
				continue
			}
			it := VerifyItem{
				Fact: a.Fact, FactBody: fn.Body, FactKind: string(fn.Kind), Why: fn.Reason,
				Source: a.Source, SrcBody: sn.Body,
				Prior: string(a.Verdict), PriorBy: a.By,
			}
			if sn.Source != nil {
				it.Class = sn.Source.Class
				if rel, err := DocPathOf(root, sn); err == nil && rel != "" {
					it.Doc = rel
					if _, serr := os.Stat(filepath.Join(root, rel)); serr == nil {
						it.Held = true
						it.Kind = sourceKind(rel)
						if it.Kind == "pdf" {
							it.Pages = pageCount(filepath.Join(root, rel))
						}
					}
				}
			}
			if a.Region != (Region{}) {
				rg := a.Region
				it.Region = &rg
			}
			for _, f := range bySource[a.Source] {
				if f != a.Fact {
					it.Related = append(it.Related, f)
				}
			}
			sort.Strings(it.Related)
			it.UsedBy = used[a.Fact]
			it.LastPage = lastPage[a.Source]
			out = append(out, it)
		}
		// Load-bearing first: a citation several documents rest on is worth a
		// person's attention before one nothing renders.
		sort.SliceStable(out, func(i, j int) bool {
			return len(out[i].UsedBy) > len(out[j].UsedBy)
		})
		writeJSON(w, out)
	})

	// The document, as an image. A scan has no text layer, so the page IS the
	// evidence and rasterising is the only way to put it on screen.
	mux.HandleFunc("/api/doc", func(w http.ResponseWriter, r *http.Request) {
		rel := r.URL.Query().Get("doc")
		if rel == "" {
			http.Error(w, "doc required", 400)
			return
		}
		abs, err := safeJoin(root, rel)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		switch sourceKind(rel) {
		case "pdf":
			png, err := rasterize(abs, page)
			if err != nil {
				http.Error(w, "rasterize: "+err.Error(), 500)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		case "image":
			http.ServeFile(w, r, abs)
		default:
			// NOT ServeFile. Handing the raw bytes back was fine for a .txt and
			// useless for everything else: a .docx went over the wire as a zip
			// container and a .doc as OLE, into an <img> that could render
			// neither. `docText` is the one place that knows how to read a
			// document — raglit for the formats it extracts, the file itself
			// when raglit does not hold it.
			txt := docText(root, rel)
			if strings.TrimSpace(txt) == "" && sourceKind(rel) == "text" {
				// `docText`'s fallback list is `{.md,.txt}` and short ON PURPOSE —
				// it drives the extraction backlog, and widening it there would
				// change what `kg extract --ready` reports. But a `.csv` a person
				// is looking at IS its own bytes, and reporting "no readable text"
				// for a file made of readable text is a lie told by a list that
				// exists for a different question. Read it here, guarded by
				// sourceKind, so a `.rdb` still says nothing rather than spraying
				// binary at the page.
				if b, rerr := os.ReadFile(filepath.Join(root, rel)); rerr == nil && utf8.Valid(b) {
					txt = string(b)
				}
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if strings.TrimSpace(txt) == "" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_, _ = w.Write([]byte(txt))
		}
	})

	// Where in the document. Lazy, because scoring every pending item up front
	// would rasterise and read four hundred documents nobody has looked at yet.
	mux.HandleFunc("/api/hints", func(w http.ResponseWriter, r *http.Request) {
		doc, fact := r.URL.Query().Get("doc"), r.URL.Query().Get("fact")
		if doc == "" || fact == "" {
			http.Error(w, "doc and fact required", 400)
			return
		}
		writeJSON(w, hintPages(fact, pageTexts(root, doc)))
	})

	// Recording a verdict. The only writer, and it appends — same contract as the
	// CLI, so a session here and a session there merge instead of clobbering.
	mux.HandleFunc("/api/attest", func(w http.ResponseWriter, r *http.Request) {
		var a Attestation
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if a.By == "" {
			a.By = by
		}
		if err := AppendAttestation(root, dir, a); err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})

	// Which documents could support this claim. Searched over the sources the
	// index already holds — registering a NEW document is a different act with
	// its own fields (class, date, medium) and does not belong behind a verdict
	// button.
	mux.HandleFunc("/api/sources", func(w http.ResponseWriter, r *http.Request) {
		p, _, err := load()
		if err != nil {
			httpErr(w, err)
			return
		}
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		type row struct {
			ID    string `json:"id"`
			Body  string `json:"body"`
			Class string `json:"class,omitempty"`
			Doc   string `json:"doc,omitempty"`
		}
		var out []row
		for id, n := range p.Graph.Nodes {
			if n == nil || n.Kind != KSource {
				continue
			}
			doc := ""
			if n.Source != nil {
				doc = n.Source.DocPath
			}
			if q != "" && !strings.Contains(strings.ToLower(id), q) &&
				!strings.Contains(strings.ToLower(n.Body), q) &&
				!strings.Contains(strings.ToLower(doc), q) {
				continue
			}
			cls := ""
			if n.Source != nil {
				cls = n.Source.Class
			}
			out = append(out, row{ID: id, Body: n.Body, Class: cls, Doc: doc})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		if len(out) > 60 {
			out = out[:60]
		}
		writeJSON(w, out)
	})

	// SUPPORTED BY — attach evidence, each edge carrying its own reason.
	//
	// Not a verdict. The four verdicts rule on a citation that already exists and
	// none of them can say what the record actually holds, so the knowledge that
	// a document supports a fact had nowhere to go but a free-text note, where it
	// stops being an edge and becomes prose nothing can query.
	//
	// It does not skip review, it CREATES it: every pair added here comes back
	// through `PendingHuman` awaiting its own ruling, because asserting that a
	// document supports a claim is not the same as having checked that it does.
	mux.HandleFunc("/api/cite", func(w http.ResponseWriter, r *http.Request) {
		if st == nil {
			http.Error(w, "this workbench was opened with --store none, which refuses every write", http.StatusConflict)
			return
		}
		var in struct {
			Fact string `json:"fact"`
			Add  []struct {
				ID      string `json:"id"`
				Because string `json:"because"`
			} `json:"add"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if in.Fact == "" || len(in.Add) == 0 {
			http.Error(w, "a fact and at least one source are required", 400)
			return
		}
		p, _, err := load()
		if err != nil {
			httpErr(w, err)
			return
		}
		for _, a := range in.Add {
			if !p.Graph.isSource(a.ID) {
				http.Error(w, fmt.Sprintf("%q is not a source in this index", a.ID), 400)
				return
			}
		}
		fields, ok, ferr := LastFields(st, in.Fact)
		if ferr != nil {
			httpErr(w, ferr)
			return
		}
		if !ok {
			http.Error(w, "no assertion of "+in.Fact+" to amend", 404)
			return
		}
		cites := make([]Cite, 0, len(in.Add))
		for _, a := range in.Add {
			cites = append(cites, Cite{ID: a.ID, Because: a.Because})
		}
		list, added := mergeCitations(fields["attested_by"], cites)
		if len(added) == 0 {
			writeJSON(w, map[string]any{"ok": true, "added": []string{}, "note": "already cited"})
			return
		}
		if _, aerr := Amend(st, p.Graph.Dialect(), in.Fact,
			map[string]any{"attested_by": list}, by, "cited from the attestation workbench"); aerr != nil {
			httpErr(w, aerr)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "added": added})
	})

	host := addr
	if h, port, err := net.SplitHostPort(addr); err == nil && (h == "" || h == "0.0.0.0") {
		host = net.JoinHostPort(lanIP(), port)
	}
	fmt.Printf("kg verify — attestation workbench\n  http://%s/\n", host)
	fmt.Printf("  signing verdicts as: %s\n", orEmpty(by))
	fmt.Println("  keys: a attested · c corrected · u unsupported · i illegible · j/k move · drag to mark the proof")
	return http.ListenAndServe(addr, mux)
}

func orEmpty(s string) string {
	if s == "" {
		return "(unsigned — pass --by)"
	}
	return s
}

// rasterize renders one PDF page. 150dpi is a deliberate floor: a scanned court
// filing at 100 is unreadable, and the region is stored in page fractions so the
// resolution can change later without moving anyone's box.
func rasterize(abs string, page int) ([]byte, error) {
	cmd := exec.Command("pdftoppm", "-png", "-r", "150",
		"-f", strconv.Itoa(page), "-l", strconv.Itoa(page), abs)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func pageCount(abs string) int {
	if !strings.EqualFold(filepath.Ext(abs), ".pdf") {
		return 1
	}
	out, err := exec.Command("pdfinfo", abs).Output()
	if err != nil {
		return 1
	}
	for _, ln := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(ln, "Pages:") {
			if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(ln, "Pages:"))); err == nil {
				return n
			}
		}
	}
	return 1
}

// safeJoin refuses a path that escapes the project. The workbench serves files
// by name from a browser on another host, so this is the boundary.
func safeJoin(root, rel string) (string, error) {
	clean := filepath.Clean(filepath.Join(root, rel))
	if !strings.HasPrefix(clean, filepath.Clean(root)+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the project")
	}
	return clean, nil
}

func lanIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, err error) { http.Error(w, err.Error(), 500) }

// Finding the place in a thirty-page scan.
//
// Showing a citation next to a whole document still leaves the hard part —
// WHERE in it. So where machine-readable text exists, score each page against
// the fact and say which pages look likely.
//
// These are HINTS and are labelled as such. A ranked guess is not a verdict, and
// the whole point of the workbench is that a person decides; a hint that quietly
// became the answer would be the machine attestation problem wearing a hat.
type Hint struct {
	Page    int     `json:"page"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

// pageTexts returns per-page text for a document, or nil when there is none.
//
// Coverage is honest rather than clever: 16 of 55 cited documents in the fence-dispute
// corpus have an OCR sidecar and NO pdf has a text layer, so most scans return
// nil and the page falls back to manual paging. Guessing from a filename or
// rasterising-then-OCRing on demand would be inventing text.
func pageTexts(root, rel string) []string {
	abs := filepath.Join(root, rel)
	ext := strings.ToLower(filepath.Ext(abs))
	if ext == ".md" || ext == ".txt" {
		if b, err := os.ReadFile(abs); err == nil {
			return splitPages(string(b))
		}
		return nil
	}
	// RAGLIT FIRST, asked for PAGES.
	//
	// This used to prefer a sidecar because the only way to ask raglit was to read
	// FRAGMENTS out of its index, and a fragment may span a page boundary while
	// carrying one page number — a page right for its start and wrong for the rest
	// is worse than none, because the workbench turns confidently to the wrong
	// page. That objection was about fragments, not about raglit: `get-document`
	// returns the per-page text produced at ingest, so the page split is raglit's
	// own and the reason to prefer a file on disk is gone.
	//
	// It also covers documents no sidecar was ever written for — .docx extracted
	// natively, audio transcribed — which is most of what a sidecar-only reader
	// could not see.
	if pp := raglitPages(root, rel); len(pp) > 0 {
		return pp
	}
	stem := strings.TrimSuffix(abs, filepath.Ext(abs))
	// `.raglit-transcription.md` first: raglit writes it from the per-page text it
	// already produced during ingest, so it is the canonical page split rather
	// than something a side script reconstructed afterwards.
	for _, cand := range []string{abs + ".raglit-transcription.md",
		stem + ".ocr.md", stem + ".text.md", abs + ".txt"} {
		b, err := os.ReadFile(cand)
		if err != nil {
			continue
		}
		if pp := splitPages(string(b)); len(pp) > 1 {
			return pp // real per-page markers
		}
	}
	for _, cand := range []string{stem + ".ocr.md", stem + ".text.md", abs + ".txt"} {
		if b, err := os.ReadFile(cand); err == nil {
			return splitPages(string(b))
		}
	}
	// A PDF with a real text layer. Most here have none, which is why this is last.
	if ext == ".pdf" {
		out, err := exec.Command("pdftotext", "-layout", abs, "-").Output()
		if err == nil && len(strings.TrimSpace(string(out))) > 200 {
			return strings.Split(string(out), "\f")
		}
	}
	return nil
}

// splitPages honours the `## Page N` markers raglit writes into an OCR sidecar,
// so a hint can name a real page rather than an offset into a blob.
func splitPages(s string) []string {
	if !strings.Contains(s, "\n## Page ") {
		return []string{s}
	}
	parts := strings.Split(s, "\n## Page ")
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		if i == 0 && !strings.HasPrefix(strings.TrimSpace(p), "## Page") {
			continue // preamble before page 1
		}
		if nl := strings.IndexByte(p, '\n'); nl >= 0 {
			p = p[nl+1:]
		}
		out = append(out, p)
	}
	return out
}

var stop = map[string]bool{"that": true, "this": true, "with": true, "from": true,
	"were": true, "have": true, "which": true, "their": true, "there": true, "been": true,
	"they": true, "them": true, "would": true, "about": true, "into": true, "than": true,
	"when": true, "what": true, "said": true, "shall": true, "will": true, "your": true,
	"defendants": true, "plaintiffs": true, "plaintiff": true, "defendant": true}

func terms(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if len(f) >= 4 && !stop[f] {
			out = append(out, f)
		}
	}
	return out
}

// hintPages ranks pages by weighted overlap with the fact. A term appearing on
// every page carries almost nothing; one appearing on a single page carries a
// lot, which is what makes a docket number or a proper noun the useful signal.
func hintPages(fact string, pages []string) []Hint {
	if len(pages) < 2 {
		return nil
	}
	low := make([]string, len(pages))
	for i, p := range pages {
		low[i] = strings.ToLower(p)
	}
	df := map[string]int{}
	uniq := map[string]bool{}
	for _, t := range terms(fact) {
		if uniq[t] {
			continue
		}
		uniq[t] = true
		for _, p := range low {
			if strings.Contains(p, t) {
				df[t]++
			}
		}
	}
	var hints []Hint
	for i, p := range low {
		var score float64
		for t := range uniq {
			if df[t] == 0 || !strings.Contains(p, t) {
				continue
			}
			score += 1.0 / float64(df[t]) // rarer term, higher weight
		}
		if score > 0 {
			hints = append(hints, Hint{Page: i + 1, Score: score, Snippet: snippet(pages[i], uniq)})
		}
	}
	sort.Slice(hints, func(a, b int) bool { return hints[a].Score > hints[b].Score })
	if len(hints) > 3 {
		hints = hints[:3]
	}
	return hints
}

// snippet returns the neighbourhood of the first matched term, so the hint can
// be judged without opening the page.
func snippet(page string, want map[string]bool) string {
	lp := strings.ToLower(page)
	best := -1
	for t := range want {
		if i := strings.Index(lp, t); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	s, e := best-90, best+150
	if s < 0 {
		s = 0
	}
	if e > len(page) {
		e = len(page)
	}
	return strings.Join(strings.Fields(page[s:e]), " ")
}

// raglitPages asks raglit for a document's per-page text.
//
// Over the HTTP API, not by reading the index. An earlier version shelled to
// `sqlite3` against `.raglit/index.sqlite` and reassembled FRAGMENTS, which is
// the arrangement `relations.go` argues against — raglit reorganised its storage
// once already and a reader of another tool's database goes quietly stale. It
// also meant page numbers taken from fragments that span pages.
//
// Unreachable degrades to nil here rather than erroring, and that is deliberate:
// this feeds a page HINT for the verification workbench, and a hint nobody can
// check is worse than paging by hand. `Have` makes the opposite choice for the
// opposite reason — it answers "do we hold this", where silence is a lie.
func raglitPages(root, rel string) []string {
	abs, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return nil
	}
	doc, err := client.New("").Document(context.Background(), raglitIndexName(root), abs)
	if err != nil || len(doc.Pages) == 0 {
		return nil
	}
	max := 0
	byPage := map[int]string{}
	for _, pg := range doc.Pages {
		n := pg.Page
		if n < 1 {
			n = 1
		}
		byPage[n] = strings.TrimSpace(byPage[n] + "\n" + pg.Text)
		if n > max {
			max = n
		}
	}
	out := make([]string, max)
	for i := 1; i <= max; i++ {
		out[i-1] = byPage[i]
	}
	return out
}

// Cite is one document being attached to a fact, with the reason it supports it.
type Cite struct {
	ID      string `json:"id"`
	Because string `json:"because"`
}

// mergeCitations adds citations to a fact's `attested_by`, carrying the existing
// ones forward EXACTLY AS AUTHORED.
//
// A bare id and a `{id, because}` mapping both parse to the same edge, so
// rewriting the untouched entries into the other form would move every one of
// their sem_hashes and flag every document that renders the fact — for a change
// nobody made. Only the new entries are written in mapping form, and only they
// carry a `because`.
//
// Returns the new list and the ids actually added; one already cited is not an
// error and not a duplicate, because `attested_by` is a set of edges and adding
// an edge twice says nothing new.
func mergeCitations(existing any, add []Cite) (list []any, added []string) {
	switch v := existing.(type) {
	case nil:
	case []any:
		list = append(list, v...)
	default:
		list = append(list, v) // a lone scalar is a one-element list
	}
	has := func(id string) bool {
		for _, e := range list {
			switch t := e.(type) {
			case string:
				if t == id {
					return true
				}
			case map[string]any:
				if t["id"] == id {
					return true
				}
			}
		}
		return false
	}
	for _, a := range add {
		if a.ID == "" || has(a.ID) {
			continue
		}
		e := map[string]any{"id": a.ID}
		if b := strings.TrimSpace(a.Because); b != "" {
			e["because"] = b
		}
		list = append(list, e)
		added = append(added, a.ID)
	}
	return list, added
}

package kgraph

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/iodesystems/raglit/client"
)

// What the content tier has actually READ, for callers above this package.
//
// THREE STATES, NOT TWO. A document may have text on disk, or text only in the
// index, or no text ANYWHERE — and the third is not a weaker form of the second.
// Measured on a real corpus: 359 of 845 undeclared documents had no text
// anywhere, mostly screenshots and un-ingested scans. A pass that reads only the
// disk reports those identically to the ones raglit has read cover to cover, and
// a person is asked to classify a document nothing can read.
//
// The third state is `not-looked` wearing a different coat, and its next step is
// INGEST rather than a ruling. Conflating it with "could not be classified" is
// the same error as conflating `not-held` with `does-not-exist`.

// DocsWithText is every document the index holds READABLE TEXT for, as
// root-relative paths. One call for the whole corpus.
//
// A nil map with a nil error means the corpus has no index — nobody ingested it,
// so there is nothing to ask and never was. An unreachable daemon is an ERROR:
// "could not ask" reported as "nothing is readable" sends somebody to ingest a
// corpus that is already ingested.
func DocsWithText(root string) (map[string]bool, error) {
	idx := raglitIndexName(root)
	if idx == "" {
		return nil, nil
	}
	docs, err := client.New("").Documents(context.Background(), idx)
	if err != nil {
		if errors.Is(err, client.ErrUnavailable) {
			return nil, fmt.Errorf("%w: %v", ErrRaglitUnavailable, err)
		}
		return nil, err
	}
	out := make(map[string]bool, len(docs))
	for _, d := range docs {
		// Fragments, not membership. Zero is a document that is in the index and
		// was never read.
		if d.Fragments == 0 {
			continue
		}
		if rel := relToRoot(root, d.Path); rel != "" && !strings.HasPrefix(rel, "..") {
			out[rel] = true
		}
	}
	return out, nil
}

// DocText is a document's text as the content tier read it, pages joined.
//
// For the document whose text exists ONLY in the index — a scan raglit OCR'd,
// with nothing on disk but the original. Empty with a nil error means the index
// has no text for it, which is an answer.
func DocText(root, rel string) (string, error) {
	idx := raglitIndexName(root)
	if idx == "" {
		return "", nil
	}
	abs, err := filepath.Abs(filepath.Join(root, rel))
	if err != nil {
		return "", err
	}
	doc, err := client.New("").Document(context.Background(), idx, abs)
	if err != nil {
		if errors.Is(err, client.ErrUnavailable) {
			return "", fmt.Errorf("%w: %v", ErrRaglitUnavailable, err)
		}
		return "", err
	}
	if doc.Text != "" {
		return doc.Text, nil
	}
	var b strings.Builder
	for _, p := range doc.Pages {
		if p.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(p.Text)
	}
	return b.String(), nil
}

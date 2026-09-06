package kgraph

import (
	"context"
	"errors"
	"fmt"

	"github.com/iodesystems/raglit/client"
)

// Withdrawn reports which documents have been ruled OUT of a corpus, and why.
//
// A withdrawal is the content tier's RULING: the file stays on disk, the grounds
// are recorded, and the ingest path honours it so the next sweep does not
// quietly put the document back. It is not a delete and it is not an absence.
//
// This exists because everything above the content tier walks the DIRECTORY. A
// consumer listing files finds a withdrawn document undeclared and offers it as
// a candidate — forever, identically on every run, with nothing anywhere saying
// a decision was made about it. Declaring one then makes the corpus able to cite
// itself, which is what the withdrawal was written to prevent, and support
// derives from it with no error and no warning. The ladder gains a rung it was
// not entitled to.
//
// Keys are paths relative to `root`, in raglit's spelling reduced the same way
// `Have` reduces a hit. Values are the recorded grounds, never empty — a
// withdrawal without them is refused at the write.
//
// NO INDEX IS NOT AN ERROR: a corpus nobody has ingested has no ledger to read
// and never had one, so this returns nil and the caller behaves as it did
// before. AN UNREACHABLE DAEMON IS an error, wrapped as ErrRaglitUnavailable —
// "nobody could ask" and "nothing was withdrawn" are the two answers this whole
// mechanism exists to keep apart, and a caller that collapses them proceeds to
// offer every withdrawn document while believing it checked.
func Withdrawn(root string) (map[string]string, error) {
	index := raglitIndexName(root)
	if index == "" {
		return nil, nil
	}
	ws, err := client.New("").Withdrawals(context.Background(), index)
	if err != nil {
		if errors.Is(err, client.ErrUnavailable) {
			return nil, fmt.Errorf("%w: %v", ErrRaglitUnavailable, err)
		}
		return nil, err
	}
	out := make(map[string]string, len(ws))
	for _, w := range ws {
		rel := relToRoot(root, w.Path)
		if rel == "" {
			continue
		}
		out[rel] = w.Reason
	}
	return out, nil
}

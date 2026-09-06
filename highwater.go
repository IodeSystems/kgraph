package kgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LIVENESS. A corpus that quietly loads as empty is the failure nothing here
// could see.
//
// The driving corpus was DARK for weeks: the parser was deleted, its 64 fact
// files stopped being read, every load built an empty graph, and every command
// reported success over it. The plan carried "750 nodes · 0 errors" the whole
// time. Nothing was broken enough to fail — which is precisely what made it
// invisible.
//
// THE MARK LIVES IN THE CORPUS, NOT THE STORE, and that placement is the whole
// design. The commonest way to load nothing is to find no store — wrong index,
// unmigrated corpus, a database that moved — so a watermark kept inside the
// store would be missing in exactly the case it exists to detect.
const highWaterName = "highwater.json"

// HighWater is the largest this index has been seen to be.
//
// It only ever goes UP. A count that could fall would record the collapse it is
// supposed to report, one run later, and then agree with it forever.
type HighWater struct {
	Nodes int    `json:"nodes"`
	Edges int    `json:"edges"`
	At    string `json:"at"`
}

// ReadHighWater loads an index's mark. Missing is not an error: a corpus nobody
// has scanned yet has no watermark, and that is the normal starting state.
func ReadHighWater(root, dir string) (HighWater, error) {
	b, err := os.ReadFile(filepath.Join(root, dir, highWaterName))
	if err != nil {
		if os.IsNotExist(err) {
			return HighWater{}, nil
		}
		return HighWater{}, err
	}
	var hw HighWater
	if err := json.Unmarshal(b, &hw); err != nil {
		return HighWater{}, fmt.Errorf("%s: %w", highWaterName, err)
	}
	return hw, nil
}

// WriteHighWater records a new mark, and only ever raises it.
//
// CALLED FROM A COMMAND, NEVER FROM A LOAD. `LoadIn` reads and compares; writing
// from a read path is how `kg query` would come to create files as a side effect
// — the same rule that keeps a scan from creating a database in `~/.kgraph`.
func WriteHighWater(root, dir string, g *Graph, at string) error {
	hw, err := ReadHighWater(root, dir)
	if err != nil {
		return err
	}
	if len(g.Nodes) <= hw.Nodes {
		return nil
	}
	b, err := json.MarshalIndent(HighWater{
		Nodes: len(g.Nodes), Edges: len(g.Edges), At: at}, "", "  ")
	if err != nil {
		return err
	}
	abs := filepath.Join(root, dir, highWaterName)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, append(b, '\n'), 0o644)
}

// CheckHighWater reports a corpus that has lost most of itself.
//
// A COLLAPSE TO ZERO IS AN ERROR, not a warning, and it is deliberately NOT
// tunable and NOT acceptable. Every other check here describes something about
// the corpus; this one says the corpus is not being READ, so every other answer
// in the run is worthless. A warning would sit in a list of 615 others.
//
// A partial drop is a warning, because it has honest causes — `kg retire` folds
// duplicates, and a matter can genuinely shed a theory.
func (g *Graph) CheckHighWater(hw HighWater) []Diag {
	if hw.Nodes == 0 {
		return nil // never recorded; nothing to compare against
	}
	now := len(g.Nodes)
	switch {
	case now == 0:
		return []Diag{{Severity: SevError, Msg: fmt.Sprintf(
			"this index loaded ZERO nodes, and it held %d on %s. Nothing has failed — that is "+
				"the point: a corpus that is not being read reports success over an empty graph. "+
				"Check that the store is where this index expects it (`kg indexes` prints it), "+
				"that the right --index is selected, and that a migration has been run. If the "+
				"emptiness is real, delete %s",
			hw.Nodes, hw.At, highWaterName)}}
	case now*2 < hw.Nodes:
		return []Diag{{Severity: SevWarn, Check: CheckGraphShrank,
			Key: hashString(fmt.Sprintf("%s\x00%d\x00%d", CheckGraphShrank, hw.Nodes, now)),
			Msg: fmt.Sprintf(
				"this index holds %d nodes and held %d on %s — less than half. Retiring duplicates "+
					"and dropping a theory both do this honestly; loading the wrong store does too",
				now, hw.Nodes, hw.At)}}
	}
	return nil
}

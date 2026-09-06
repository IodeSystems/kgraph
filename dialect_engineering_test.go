package kgraph

import "testing"

// The built-in guard already runs validateDialect over this at init, so a
// malformed ladder would panic before any test ran. These are the claims the
// validator cannot make: that the ORDER is the one intended, and that the
// mechanism the order exists for actually follows from it.
func TestEngineeringLadderOrdersByWarrant(t *testing.T) {
	d, ok := DialectFor("engineering")
	if !ok {
		t.Fatal("engineering is not registered")
	}
	// How a thing was found out, strongest first. A number that carries its
	// method beats an unaided observation of the same behaviour.
	for _, pair := range [][2]string{
		{"measured", "observed"},
		{"observed", "signed-off"},
		{"signed-off", "accepted"},
		{"accepted", "reported"},
		{"reported", "inference"},
	} {
		hi, hiOK := d.Rank(pair[0])
		lo, loOK := d.Rank(pair[1])
		if !hiOK || !loOK {
			t.Fatalf("%q or %q is not on the ladder", pair[0], pair[1])
		}
		if hi <= lo {
			t.Errorf("%q (%d) must outrank %q (%d)", pair[0], hi, pair[1], lo)
		}
	}
}

// The load-bearing choice. A preference is a rule, not evidence: conflict looks
// up a rank and skips what has none, so a decision settles a question without
// having to beat a measurement in an evidentiary contest. If `decided` ever
// gained a rank, a benchmark could outrank what the user asked for.
func TestDecidedGovernsAndHasNoRank(t *testing.T) {
	d, _ := DialectFor("engineering")
	if d.Governs() != "decided" {
		t.Fatalf("governs %q, want decided", d.Governs())
	}
	if _, ranked := d.Rank("decided"); ranked {
		t.Fatal("the governing class must have no rank — with one it can win or lose an evidentiary contest")
	}
}

// "Accepted" means nothing without by whom: an orchestrator accepting its own
// work is the failure a review layer exists to prevent, and a graph that could
// not name the reviewer could not tell that case from a real one.
func TestReviewRungsRequireASpeaker(t *testing.T) {
	d, _ := DialectFor("engineering")
	for _, c := range []string{"reported", "accepted", "signed-off"} {
		if !d.SpeakerRequired(c) {
			t.Errorf("%q must name who said it", c)
		}
	}
	// A measurement is about the world, not about anyone's position in it.
	for _, c := range []string{"measured", "observed", "inference"} {
		if d.SpeakerRequired(c) {
			t.Errorf("%q should not require a speaker", c)
		}
	}
}

// The word has to say which failure it was: an impeached source is argued
// about, a stale one is re-measured.
func TestInvalidationIsStaleness(t *testing.T) {
	d, _ := DialectFor("engineering")
	if got := d.Invalidated(); got != "stale" {
		t.Fatalf("invalidated flag is %q, want stale", got)
	}
	if legalD, _ := DialectFor("legal"); legalD.Invalidated() == d.Invalidated() {
		t.Fatal("the mechanic is core but the word is dialect-scoped, and these two differ")
	}
}

// A number with no method and no date is not a measurement in any trade.
func TestMeasurementCarriesItsMethod(t *testing.T) {
	d, _ := DialectFor("engineering")
	st, ok := d.Subtype("measurement")
	if !ok {
		t.Fatal("no measurement subtype")
	}
	if st.Kind() != KClaim {
		t.Fatalf("measurement is a %q, want a claim", st.Kind())
	}
	for _, k := range []string{"how", "when"} {
		if !st.Governs(k) {
			t.Errorf("measurement should carry %q", k)
		}
	}
	// NOT `value`. A claim already carries one, so a subtype declaring it would
	// shadow a core scalar — refused at init, and rightly: the key would be
	// ambiguous on any node that did not prefix it. The method and the date are
	// what a core node has nowhere to put.
	if st.Governs("value") {
		t.Error("measurement must not shadow the core `value` field")
	}
}

// A built-in hashes to the empty string, so a corpus that binds it records
// exactly as it did before dialects were data and nothing false-flags.
func TestBuiltInHashesEmpty(t *testing.T) {
	d, _ := DialectFor("engineering")
	if d.hash != "" {
		t.Fatalf("a built-in must not hash: %q", d.hash)
	}
}

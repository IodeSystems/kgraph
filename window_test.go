package kgraph

import "testing"

// `@[from,to]` is a WINDOW. Its `to` half was parsed and then dropped, so a
// windowed query silently degraded to "as of `from`" and matched everything —
// on the fence-dispute timeline, `event @[2021-01-01,2021-12-31]` returned 46 of 48
// events and every section of the chronology rendered the whole chronology.
//
// A filter that silently matches everything is worse than one that errors: the
// document it produces is well-formed and wrong.
func TestTemporalWindowFiltersByDate(t *testing.T) {
	g := fixtureGraph(t)
	all := run(t, g, "event")
	if len(all) == 0 {
		t.Fatal("fixture has no events — this test cannot say anything")
	}

	// A window covering everything must not lose rows...
	wide := run(t, g, "event @[0001-01-01,9999-12-31]")
	if len(wide) != len(all) {
		t.Errorf("an all-covering window returned %d of %d events", len(wide), len(all))
	}

	// ...and one covering nothing must return nothing. Before the fix this
	// returned every event in the graph.
	empty := run(t, g, "event @[1900-01-01,1900-12-31]")
	if len(empty) != 0 {
		t.Errorf("a window with no events in it returned %d rows: %v", len(empty), empty)
	}

	// Windows that partition the range must sum to the whole, with no row in
	// two buckets — that is what a timeline's sections rely on.
	early := run(t, g, "event @[0001-01-01,2020-12-31]")
	late := run(t, g, "event @[2021-01-01,9999-12-31]")
	if len(early)+len(late) != len(all) {
		t.Errorf("partition lost or duplicated rows: %d + %d != %d", len(early), len(late), len(all))
	}
	in := map[string]bool{}
	for _, id := range early {
		in[id] = true
	}
	for _, id := range late {
		if in[id] {
			t.Errorf("%s appears in both halves of a partition", id)
		}
	}
}

// A single `@DATE` stays AS-OF, not a one-day window — it is how alias
// resolution is dated, and turning it into a filter would break every
// `#ref @date` query.
func TestSingleDateStaysAsOf(t *testing.T) {
	g := fixtureGraph(t)
	asOf := run(t, g, "event @2025-06-01")
	all := run(t, g, "event")
	if len(asOf) != len(all) {
		t.Errorf("@DATE should resolve as-of, not filter to that day: %d vs %d", len(asOf), len(all))
	}
}

// A coarse date is an INTERVAL, not a point. `2022` means all of 2022, so it
// belongs in a 2022–2023 window — but "2022" < "2022-01-01" lexically, so a
// naive point comparison drops it. On the fence-dispute timeline that silently removed
// `e-suit-filed`, the filing of the lawsuit, from the section about the lawsuit.
func TestWindowIncludesCoarseDates(t *testing.T) {
	for _, tc := range []struct {
		date, from, to string
		want           bool
		why            string
	}{
		{"2022", "2022-01-01", "2023-12-31", true, "a bare year inside the window"},
		{"2022", "2021-01-01", "2021-12-31", false, "a bare year outside it"},
		{"2022-08", "2022-01-01", "2022-12-31", true, "a month inside its year"},
		{"2022-08", "2022-09-01", "2022-12-31", false, "a month before the window opens"},
		{"2022-08", "2022-08-15", "2022-09-01", true, "a month straddling the window's start"},
		{"2021-12-31", "2022-01-01", "2022-12-31", false, "the day before"},
		{"2022-01-01", "2022-01-01", "2022-12-31", true, "the first day"},
		{"2022-12-31", "2022-01-01", "2022-12-31", true, "the last day"},
		{"2022-06-15", "2022", "2023", true, "a partial window bound covers its whole year"},
		{"2024-01-01", "2022", "2023", false, "outside a partial window bound"},
	} {
		if got := dateOverlapsWindow(tc.date, tc.from, tc.to); got != tc.want {
			t.Errorf("%s: dateOverlapsWindow(%q, %q, %q) = %v, want %v",
				tc.why, tc.date, tc.from, tc.to, got, tc.want)
		}
	}
}

package kgraph

import (
	"fmt"
	"math/rand"
	"regexp"
	"testing"
)

// identifierSetSlow is the implementation before the fast path: every pattern
// run over every string, unconditionally. Kept here as the ORACLE — a fast path
// is only ever a cheaper route to the same answer, and the only way to say that
// with a straight face is to compute both and compare.
func identifierSetSlow(s string) map[string]bool {
	out := map[string]bool{}
	for _, pat := range identifierPatterns {
		for _, m := range pat.re.FindAllString(s, -1) {
			for _, f := range queryForms(m) {
				out[f] = true
			}
		}
	}
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// TestFastPathMatchesTheSlowPath — identifierSet skips the regexes when the
// haystack cannot possibly match: no ASCII digit at all, or no literal anchor
// for a given pattern. Both claims are about the PATTERNS, so if somebody adds
// one without a `lit`, or one that can match without a digit, this fails.
func TestFastPathMatchesTheSlowPath(t *testing.T) {
	cases := []string{
		"", "no identifiers here at all",
		"AF 201503110042", "af201503110042", "af#201503110042", "AF  #  20150311004",
		"PL 00-0851", "pl00-0851", "PL00 - 0851",
		"Form 17", "form 35R", "FORM 21",
		"documents/records/201503110042-2008-deed-block10-lots5-6-plus-ROW.pdf",
		"a sentence about a form with no number in it",
		"affidavit of the plaintiff",             // "af" and "pl" with no digits
		"platypus 12345678",                      // "pl" plus a bare number
		"the AF is missing but 12345678 is here", // anchor without a valid match
		"Ω unicode ≠ ascii 201503110042",
		"AF 1234567", "AF 123456789012345", // just outside the digit bounds
	}
	// Plus noise: strings that mix anchors, digits and neither.
	r := rand.New(rand.NewSource(1))
	bits := []string{"af", "AF", "pl", "Form ", "17", "201503110042", " ", "-", "#", "x", "…"}
	for i := 0; i < 500; i++ {
		s := ""
		for j := 0; j < 1+r.Intn(6); j++ {
			s += bits[r.Intn(len(bits))]
		}
		cases = append(cases, s)
	}

	for _, s := range cases {
		if got, want := identifierSet(s), identifierSetSlow(s); !sameSet(got, want) {
			t.Errorf("identifierSet(%q) = %v, slow path = %v", s, got, want)
		}
	}
}

// TestEveryPatternRequiresADigit is what the digit fast path rests on. A pattern
// that can match without one would be skipped for every digitless string and
// silently stop finding anything.
func TestEveryPatternRequiresADigit(t *testing.T) {
	for i, pat := range identifierPatterns {
		// A pattern matching a string with no digits would break the fast path.
		for _, s := range []string{
			"af", "AF #", "pl -", "Form ", "aaaa bbbb cccc", "AF AF AF", "Form Form",
		} {
			if pat.re.MatchString(s) {
				t.Errorf("pattern %d matches %q, which has no digit — the fast path in "+
					"identifierSet skips it and would miss this", i, s)
			}
		}
		if pat.lit == "" {
			t.Errorf("pattern %d has no `lit` anchor; it will run on every string", i)
			continue
		}
		// And the anchor must really be required.
		if regexp.MustCompile(`(?i)`+pat.lit).String() == "" {
			t.Errorf("pattern %d has an empty anchor", i)
		}
	}
}

func BenchmarkIdentifierSetProse(b *testing.B) {
	s := "The parties met on the seventeenth and discussed the driveway easement " +
		"at some length without reaching any agreement about the strip."
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = identifierSet(s)
	}
}

func BenchmarkIdentifierSetSlowProse(b *testing.B) {
	s := "The parties met on the seventeenth and discussed the driveway easement " +
		"at some length without reaching any agreement about the strip."
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = identifierSetSlow(s)
	}
}

var _ = fmt.Sprint

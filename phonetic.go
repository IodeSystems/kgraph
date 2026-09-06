package kgraph

import "strings"

// PHONETIC MATCHING, FOR NAMES ONLY.
//
// A surname survives OCR and transcription badly and predictably: doubled
// letters collapse, vowels wander, `ph`/`f` and `c`/`k` swap, a trailing `e`
// appears or leaves. Two spellings of one person are then two entries in a
// catalogue and the second is invisible to anyone searching for the first.
//
// SCOPED TO NAMES, AND NEVER TO IDENTIFIERS, which is the whole safety of it.
// An auditor's file number, a form number and a certification number are exact
// strings where a near miss is a DIFFERENT INSTRUMENT — matching `201503110043`
// to `201503110044` phonetically would be worse than not matching at all. Those
// are matched by `normalize` and exact containment, and this never sees them.

// soundsLike is a compact phonetic key: a Soundex variant that keeps more of the
// consonant skeleton than the classic four-character form, because a legal
// corpus holds surnames that collide at four (`Barlow`/`Borley`) and being told
// two different people are one is worse than missing a variant.
//
// Digits and anything non-alphabetic yield no key at all — that is what keeps
// this off identifiers even if it is ever called with one.
func soundsLike(s string) string {
	var letters []rune
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z':
			letters = append(letters, r)
		case r >= '0' && r <= '9':
			// A NUMBER IS NOT A NAME. One digit anywhere and this is an
			// identifier, not something to sound out.
			return ""
		}
	}
	if len(letters) < 3 {
		return "" // too short to have a stable sound
	}
	code := map[rune]byte{
		'b': '1', 'f': '1', 'p': '1', 'v': '1',
		'c': '2', 'g': '2', 'j': '2', 'k': '2', 'q': '2', 's': '2', 'x': '2', 'z': '2',
		'd': '3', 't': '3',
		'l': '4',
		'm': '5', 'n': '5',
		'r': '6',
	}
	var b strings.Builder
	b.WriteRune(letters[0])
	var last byte
	if c, ok := code[letters[0]]; ok {
		last = c
	}
	for _, r := range letters[1:] {
		c, ok := code[r]
		if !ok {
			// A vowel (or h/w) breaks the run, so a doubled consonant either side
			// of one still collapses but two genuinely separate sounds do not.
			last = 0
			continue
		}
		if c != last {
			b.WriteByte(c)
			last = c
		}
	}
	// Six, not the classic four. That is not on its own enough — a consonant
	// skeleton collides by construction, and `Barlow` and `Borley` both code to
	// `b64` however long the key is kept — which is why the key is only a
	// CANDIDATE FILTER and `soundsAlike` confirms with an edit distance.
	k := b.String()
	if len(k) > 6 {
		k = k[:6]
	}
	return k
}

// soundsAlike reports whether two strings are the same name, word for word, when
// sounded out.
//
// WORD BY WORD rather than as one run, so "Sally Tritt-Collins" and "Sally
// Tritt Collins" agree while "Collins Sally" does not — a name is an ordered
// thing, and a corpus that matched any permutation would join a mother and
// daughter who share a surname.
func soundsAlike(a, b string) bool {
	x, y := nameKeys(a), nameKeys(b)
	if len(x) == 0 || len(x) != len(y) {
		return false
	}
	aw, bw := nameFields(a), nameFields(b)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
		// THE KEY IS A FILTER, NOT THE VERDICT. A consonant skeleton collides by
		// construction: `Barlow` and `Borley` both code to `b64`, and calling two
		// parties one person is a worse failure than missing a spelling variant.
		// So the spelling has to be close too — an OCR variant differs by a letter
		// or two, a different surname by more.
		if editDistance(strings.ToLower(aw[i]), strings.ToLower(bw[i])) > maxNameEdits {
			return false
		}
	}
	return true
}

// maxNameEdits is how far two spellings of one name may be. Two covers the
// variants that actually occur — a doubled letter, a wandering vowel, a trailing
// e, a dropped consonant — and stops short of `Barlow`/`Borley`, which is three.
const maxNameEdits = 2

// editDistance is Levenshtein, bounded by the shorter string.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			m := prev[j] + 1
			if cur[j-1]+1 < m {
				m = cur[j-1] + 1
			}
			if prev[j-1]+cost < m {
				m = prev[j-1] + cost
			}
			cur[j] = m
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// nameFields splits a name the same way nameKeys does, so word i of one lines up
// with word i of the other.
func nameFields(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
}

// nameKeys is the phonetic key of each word, or nil if any word has no key —
// one digit, or one word too short to sound out, and the whole string is not a
// name this may rule on.
func nameKeys(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		k := soundsLike(f)
		if k == "" {
			return nil
		}
		out = append(out, k)
	}
	return out
}

// soundsAlikeIn reports whether a name is spoken ANYWHERE in a phrase.
//
// `soundsAlike` compares two whole strings, which is right for two names and
// useless against real data: an alias is `Bruce Harrow (the surveyor)` and a
// description is a sentence, so a query of one surname never lined up and the
// tier never fired on the live corpus at all. Measured, not assumed — "Lissar"
// against a corpus holding that surname returned nothing.
//
// So the query slides over the phrase's words. The per-word edit-distance floor
// still applies at every position, which is what stops a longer phrase from
// eventually matching anything by chance.
func soundsAlikeIn(phrase, query string) bool {
	qk := nameKeys(query)
	if len(qk) == 0 {
		return false
	}
	pw := nameFields(phrase)
	if len(pw) < len(qk) {
		return false
	}
	for i := 0; i+len(qk) <= len(pw); i++ {
		if soundsAlike(strings.Join(pw[i:i+len(qk)], " "), query) {
			return true
		}
	}
	return false
}

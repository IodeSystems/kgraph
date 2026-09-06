package kgraph

import "testing"

// A NAME SURVIVES OCR BADLY AND PREDICTABLY. Two spellings of one person become
// two entries in a catalogue, and the second is invisible to anyone searching
// for the first.
func TestASurnameMatchesItsOcrVariants(t *testing.T) {
	same := [][2]string{
		{"Harrow", "Harrowe"},              // a trailing e
		{"Tritt-Collins", "Tritt Collins"}, // punctuation
		{"Anabel", "Annabel"},              // a doubled letter
		{"Sally Tritt", "Sally Trit"},
	}
	for _, p := range same {
		if !soundsAlike(p[0], p[1]) {
			t.Errorf("%q and %q are one name written two ways (%q vs %q)",
				p[0], p[1], soundsLike(p[0]), soundsLike(p[1]))
		}
	}

	// AND TWO DIFFERENT PEOPLE MUST STAY DIFFERENT. Telling somebody that two
	// parties are one is a worse failure than missing a spelling variant, which
	// is why the key keeps six characters rather than Soundex's four.
	diff := [][2]string{
		{"Barlow", "Borley"},
		{"Wren", "Wrenly"},
		{"Sally Collins", "Collins Sally"}, // a name is ordered
		{"Halloway", "Holloway Jr"},        // a different number of words
	}
	for _, p := range diff {
		if soundsAlike(p[0], p[1]) {
			t.Errorf("%q and %q were called one name (both %q)", p[0], p[1], soundsLike(p[0]))
		}
	}
}

// AN IDENTIFIER IS NEVER SOUNDED OUT, and this is the whole safety of the tier.
//
// An auditor's file number, a form number and a certification number are exact
// strings where a near miss is a DIFFERENT INSTRUMENT. Matching 201503110043 to
// 201503110044 by sound would be worse than not matching at all.
func TestAnIdentifierIsNeverMatchedBySound(t *testing.T) {
	for _, id := range []string{
		"201503110043", "AF 201503110043", "PL15-0311", "Form 22J", "H19-440",
	} {
		if k := soundsLike(id); k != "" {
			t.Errorf("%q got a phonetic key %q — one digit and it is an instrument, not a name", id, k)
		}
		if soundsAlike(id, "201503110044") {
			t.Errorf("%q was matched to a different number by sound", id)
		}
	}
	// The composite case: a real title mixing a name and a number must not be
	// sounded out either, because the number is the part that identifies it.
	if soundsLike("Crestline survey 201503110043") != "" {
		t.Error("a title carrying an instrument number was sounded out")
	}
}

// The phonetic tier ranks BELOW every exact tier: a match by sound is weaker
// evidence than any spelling that actually agrees.
func TestSoundsRanksBelowEveryExactTier(t *testing.T) {
	for _, exact := range []string{"alias", "source", "path"} {
		if viaRank[exact] >= viaRank["sounds"] {
			t.Errorf("%q does not outrank a phonetic match", exact)
		}
	}
	if viaRank["sounds"] >= viaRank["text"] {
		t.Error("a phonetic match should outrank a document that merely shares vocabulary")
	}
}

// A NAME IS SPOKEN INSIDE A PHRASE, which is the only form real data takes.
//
// `soundsAlike` compares two whole strings, which is right for two names and
// useless against a corpus: an alias is "Bruce Harrow (the surveyor)" and a
// description is a sentence, so a one-word query never lined up and the tier
// never fired at all. Measured on the live corpus before this — a misspelled
// surname returned nothing, against a corpus holding that surname.
func TestANameIsFoundInsideAPhrase(t *testing.T) {
	const alias = "Record of survey AF 201907220015, Harrow & Associates, 2022"
	if !soundsAlikeIn(alias, "Harrowe") {
		t.Error("a misspelled surname did not match the phrase containing it")
	}
	if !soundsAlikeIn("Letter, Wren to Taylor re the easement", "Wren") {
		t.Error("a surname mid-phrase was not found")
	}
	// AND SLIDING MUST NOT MAKE IT PROMISCUOUS. A longer phrase gives more
	// positions to match at, and the per-word edit floor is what stops one of
	// them eventually succeeding by chance.
	if soundsAlikeIn(alias, "Borley") {
		t.Error("a different surname matched by sliding")
	}
	if soundsAlikeIn(alias, "201907220016") {
		t.Error("a near-miss instrument number matched inside a phrase")
	}
	// A query longer than the phrase cannot match.
	if soundsAlikeIn("Harrow", "Bruce Harrow the surveyor") {
		t.Error("a query longer than the phrase matched")
	}
}

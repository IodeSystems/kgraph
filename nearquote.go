package kgraph

import (
	"strings"
	"unicode"
)

// A QUOTE CAN BE WRONG WITHOUT BEING INVENTED, and "absent" was reporting both.
//
// Measured on the live corpus: of 14 absent quotations, several differ from
// their document by ONE WORD — `impute` where the opinion says `imputes`, "there
// is" where a verified transcript says "there's". The rest are genuinely not
// there at all. Those are opposite findings with opposite fixes — correct the
// quotation, versus find out what the document really says — and reporting them
// identically sends a person to read a thirty-page scan either way.
//
// So a near miss is reported as NEAR, with the passage it nearly matches, and
// the difference is shown. That turns a document-reading task into a one-line
// comparison, which is the same leverage `FoundIn` gives for a citation on the
// wrong document.

// nearThreshold is how alike a passage must be to be called a near miss.
//
// 0.75 of the words in common, measured as a subsequence rather than a set: word
// ORDER is most of what makes a quotation a quotation, and a bag-of-words
// measure would call a paraphrase with the same vocabulary an exact hit. Live
// corpus at this setting: the one-word differences land at 0.87–0.88 and the
// genuinely absent at 0.29–0.46, so the gap either side of it is wide.
const nearThreshold = 0.75

// nearMiss finds the passage in `text` that most resembles `quote`, when one
// resembles it enough to be worth showing.
//
// ANCHORED ON THE QUOTE'S RAREST LONG WORD rather than scanned window by window.
// A transcription runs to megabytes and a quotation to a line; comparing every
// window would be quadratic in the wrong variable. A near miss shares nearly all
// its words with the passage, so at least one distinctive word lands — and if
// none does, it was never a near miss.
func nearMiss(quote, text string) (passage string, ratio float64) {
	q := quoteWords(quote)
	if len(q) < 4 {
		return "", 0 // too short to be distinctive; the exact match already ruled
	}
	t := quoteWords(text)
	if len(t) == 0 {
		return "", 0
	}
	anchors := rarestWords(q, 3)
	at := map[string][]int{}
	for i, w := range t {
		for _, a := range anchors {
			if w == a {
				at[a] = append(at[a], i)
			}
		}
	}
	best, bestAt, bestLen := 0.0, -1, 0
	for _, a := range anchors {
		for _, pos := range at[a] {
			// The anchor can sit anywhere in the quote, so try the window that
			// would put it where it sits in the quote, with slack either side.
			for _, off := range anchorOffsets(q, a) {
				start := pos - off
				if start < 0 {
					start = 0
				}
				end := start + len(q)
				if end > len(t) {
					end = len(t)
				}
				r := wordRatio(q, t[start:end])
				if r > best {
					best, bestAt, bestLen = r, start, end-start
				}
			}
		}
	}
	if best < nearThreshold || bestAt < 0 {
		return "", best
	}
	return strings.Join(t[bestAt:bestAt+bestLen], " "), best
}

// quoteWords is the comparison unit: lowercase alphanumeric words.
//
// The same normalisation `foldText` applies — including skipping a reporter's
// star pagination — but kept as WORDS rather than one run of characters, so a
// difference can be reported as "this word, not that one".
func quoteWords(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	skipTo := -1
	for i, r := range s {
		if i < skipTo {
			continue
		}
		if r == '*' {
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j > i+1 {
				skipTo = j
				flush()
				continue
			}
		}
		l := unicode.ToLower(r)
		if unicode.IsLetter(l) || unicode.IsDigit(l) {
			b.WriteRune(l)
			continue
		}
		flush()
	}
	flush()
	return out
}

// rarestWords picks the longest words, as a cheap stand-in for distinctiveness:
// a long word is unlikely to be "the".
func rarestWords(ws []string, n int) []string {
	seen := map[string]bool{}
	var cand []string
	for _, w := range ws {
		if len(w) >= 6 && !seen[w] {
			seen[w] = true
			cand = append(cand, w)
		}
	}
	// Longest first, and stable.
	for i := 1; i < len(cand); i++ {
		for j := i; j > 0 && len(cand[j]) > len(cand[j-1]); j-- {
			cand[j], cand[j-1] = cand[j-1], cand[j]
		}
	}
	if len(cand) > n {
		cand = cand[:n]
	}
	return cand
}

func anchorOffsets(q []string, a string) []int {
	var out []int
	for i, w := range q {
		if w == a {
			out = append(out, i)
		}
	}
	return out
}

// wordRatio is 2·LCS / (len(a)+len(b)) — the same shape as a diff ratio, over
// words. Order-sensitive on purpose: word order is most of what makes a
// quotation a quotation.
func wordRatio(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
		for k := range cur {
			cur[k] = 0
		}
	}
	return 2 * float64(prev[len(b)]) / float64(len(a)+len(b))
}

// QuoteDiff is the words that differ between a quotation and the passage it
// nearly matches, as "-authored +document".
func QuoteDiff(quote, passage string) string {
	q, p := quoteWords(quote), quoteWords(passage)
	inP := map[string]int{}
	for _, w := range p {
		inP[w]++
	}
	inQ := map[string]int{}
	for _, w := range q {
		inQ[w]++
	}
	var out []string
	for _, w := range q {
		if inP[w] == 0 {
			out = append(out, "-"+w)
			inP[w]--
		}
	}
	for _, w := range p {
		if inQ[w] == 0 {
			out = append(out, "+"+w)
			inQ[w]--
		}
	}
	if len(out) > 12 {
		out = append(out[:12], "…")
	}
	return strings.Join(out, " ")
}

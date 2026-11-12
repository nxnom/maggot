// Package search ranks tools against a free-text query with plain word
// matching over the manifest. No index or embeddings: toolboxes are small.
package search

import (
	"sort"
	"strings"
	"unicode"

	"github.com/nxnom/maggot/internal/toolbox"
)

type Hit struct {
	Tool    *toolbox.Tool
	Score   int
	Matched []string // query words that matched
}

var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "to": true, "for": true, "of": true, "and": true, "or": true,
	"in": true, "into": true, "on": true, "with": true, "from": true, "by": true, "my": true, "me": true,
	"i": true, "it": true, "is": true, "tool": true, "tools": true, "some": true, "this": true, "that": true,
}

// Words splits text into lowercase words and drops stop words.
func Words(s string) []string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := f[:0]
	for _, w := range f {
		if !stopWords[w] {
			out = append(out, w)
		}
	}
	return out
}

// wordMatch is true for equal words, or when one is a prefix of the other,
// the shorter has 3+ letters and covers most of the longer ("remove" ~
// "remover", "pdf" ~ "pdfs"; "don" is not "donwload").
func wordMatch(q, w string) bool {
	if q == w {
		return true
	}
	short, long := q, w
	if len(short) > len(long) {
		short, long = long, short
	}
	return len(short) >= 3 && len(short)*10 >= len(long)*6 && strings.HasPrefix(long, short)
}

type field struct {
	weight int
	words  []string
}

func fields(t *toolbox.Tool) []field {
	m := t.Manifest
	var inputs, examples []string
	for _, in := range m.Inputs {
		inputs = append(inputs, in.Name, in.Label, in.Description)
	}
	examples = append(examples, m.Examples...)
	return []field{
		{6, Words(m.ID)},
		{5, Words(m.Name)},
		{4, Words(strings.Join(m.Tags, " "))},
		{3, Words(strings.Join(m.Categories, " "))},
		{3, Words(m.Description)},
		{1, Words(strings.Join(inputs, " "))},
		{1, Words(strings.Join(examples, " "))},
	}
}

// Rank returns tools that match at least one query word, best first: more
// matched words first, then higher score, then id.
func Rank(tools []*toolbox.Tool, query string) []Hit {
	q := Words(query)
	if len(q) == 0 {
		return nil
	}
	var hits []Hit
	for _, t := range tools {
		fs := fields(t)
		h := Hit{Tool: t}
		for _, qw := range q {
			best := 0
			for _, f := range fs {
				for _, w := range f.words {
					if wordMatch(qw, w) && f.weight > best {
						best = f.weight
						if qw == w {
							best++ // exact word beats prefix
						}
					}
				}
			}
			if best > 0 {
				h.Score += best
				h.Matched = append(h.Matched, qw)
			}
		}
		if h.Score > 0 {
			hits = append(hits, h)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if len(a.Matched) != len(b.Matched) {
			return len(a.Matched) > len(b.Matched)
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.Tool.Ref() < b.Tool.Ref()
	})
	return hits
}

// grams returns the 3-letter pieces of one word, with word edges marked.
func grams(w string) map[string]bool {
	out := map[string]bool{}
	w = " " + w + " "
	for i := 0; i+3 <= len(w); i++ {
		out[w[i:i+3]] = true
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	common := 0
	for g := range a {
		if b[g] {
			common++
		}
	}
	if common == 0 {
		return 0
	}
	return float64(common) / float64(len(a)+len(b)-common)
}

// minWordSimilarity is how alike a query word and a tool word must be to
// count as a near match ("yotube" ~ "youtube" is 0.44, unrelated words ~0).
const minWordSimilarity = 0.3

// Closest finds tools whose words look like the query words, for when Rank
// finds nothing (typos, other word forms). A tool qualifies only when at
// least half of the query words are near a word of the tool, so unrelated
// queries return nothing.
func Closest(tools []*toolbox.Tool, query string, n int) []*toolbox.Tool {
	q := Words(query)
	if len(q) == 0 {
		return nil
	}
	type scored struct {
		t     *toolbox.Tool
		score float64
	}
	var all []scored
	for _, t := range tools {
		m := t.Manifest
		words := Words(strings.Join(append([]string{m.ID, m.Name, m.Description}, append(m.Tags, m.Categories...)...), " "))
		docGrams := make([]map[string]bool, len(words))
		for i, w := range words {
			docGrams[i] = grams(w)
		}
		near, total := 0, 0.0
		for _, qw := range q {
			qg := grams(qw)
			best := 0.0
			for _, dg := range docGrams {
				if s := jaccard(qg, dg); s > best {
					best = s
				}
			}
			if best >= minWordSimilarity {
				near++
				total += best
			}
		}
		if near > 0 && near*2 >= len(q) {
			all = append(all, scored{t, total / float64(len(q))})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	var out []*toolbox.Tool
	for _, s := range all {
		if len(out) == n {
			break
		}
		out = append(out, s.t)
	}
	return out
}

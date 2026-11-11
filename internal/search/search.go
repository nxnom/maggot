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

// wordMatch is true for equal words, or when one is a prefix of the other and
// the shorter has 3+ letters ("remove" ~ "remover", "img" ~ "image" is not).
func wordMatch(q, w string) bool {
	if q == w {
		return true
	}
	short, long := q, w
	if len(short) > len(long) {
		short, long = long, short
	}
	return len(short) >= 3 && strings.HasPrefix(long, short)
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

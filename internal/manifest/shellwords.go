package manifest

import (
	"errors"
	"strings"
)

// SplitArgs splits a command line like a POSIX shell does for words: spaces
// separate words, single quotes are literal, double quotes allow \" and \\,
// and a backslash outside quotes escapes the next character. No variables,
// globs or command substitution.
func SplitArgs(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inWord := false
	const (
		none = iota
		single
		double
	)
	quote := none
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch quote {
		case single:
			if r == '\'' {
				quote = none
			} else {
				cur.WriteRune(r)
			}
		case double:
			switch {
			case r == '"':
				quote = none
			case r == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\'):
				i++
				cur.WriteRune(runes[i])
			default:
				cur.WriteRune(r)
			}
		default:
			switch {
			case r == ' ' || r == '\t' || r == '\n' || r == '\r':
				if inWord {
					out = append(out, cur.String())
					cur.Reset()
					inWord = false
				}
			case r == '\'':
				quote, inWord = single, true
			case r == '"':
				quote, inWord = double, true
			case r == '\\':
				if i+1 < len(runes) {
					i++
					cur.WriteRune(runes[i])
				}
				inWord = true
			default:
				cur.WriteRune(r)
				inWord = true
			}
		}
	}
	if quote != none {
		return nil, errors.New("unclosed quote")
	}
	if inWord {
		out = append(out, cur.String())
	}
	return out, nil
}

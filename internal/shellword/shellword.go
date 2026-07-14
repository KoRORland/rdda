// Package shellword splits a command string into an argv the way a POSIX shell
// tokenizes a simple command: whitespace separates words, single quotes are
// literal, double quotes group while allowing \" and \\, and a backslash escapes
// the next character outside single quotes. It intentionally does NOT expand
// variables, globs, or run any shell — it only parses, so an operator's
// `--reload-cmd` with a spaced path (`/opt/my tools/reload.sh`) survives instead
// of being mis-split by strings.Fields. (F-5)
package shellword

import "fmt"

// Split tokenizes s into words. It errors on an unterminated quote rather than
// guessing, so a malformed command surfaces loudly instead of running a
// truncated argv.
func Split(s string) ([]string, error) {
	var (
		words   []string
		cur     []rune
		inWord  bool
		quote   rune // 0, '\'' or '"'
		escaped bool
	)
	flush := func() {
		words = append(words, string(cur))
		cur = cur[:0]
		inWord = false
	}
	for _, r := range s {
		switch {
		case escaped:
			cur = append(cur, r)
			escaped = false
			inWord = true
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur = append(cur, r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur = append(cur, r)
			}
		case r == '\\':
			escaped = true
			inWord = true
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inWord {
				flush()
			}
		default:
			cur = append(cur, r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if escaped {
		return nil, fmt.Errorf("trailing backslash")
	}
	if inWord {
		flush()
	}
	return words, nil
}

package ledger

import (
	"regexp"
	"strings"
)

// mentionRE matches @<word> where the @ is at word start (preceded by
// whitespace or beginning-of-string) — rejects email-like patterns.
var mentionRE = regexp.MustCompile(`(?:^|\s)@([A-Za-z][A-Za-z0-9_-]*)`)

// ParseMentions returns unique lowercased aspect names referenced in
// the markdown text. Case-insensitive per spec.
func ParseMentions(text string) []string {
	matches := mentionRE.FindAllStringSubmatch(text, -1)
	seen := map[string]struct{}{}
	var out []string
	for _, m := range matches {
		name := strings.ToLower(m[1])
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

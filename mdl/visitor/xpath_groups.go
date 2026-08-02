// SPDX-License-Identifier: Apache-2.0

package visitor

import "strings"

// SplitXPathPredicateGroups splits a stored XPathConstraint into its top-level
// predicate groups, each returned with its enclosing brackets. Mendix concatenates
// sibling groups — `[a][b][c]`, often separated by newlines — and the whole string
// is what the BSON field holds.
//
// Splitting on "][" is not enough: a group may nest brackets of its own
// (`[Mod.Assoc/Mod.Entity[EndDate = $Limit]]`), and a string literal may contain a
// bracket (`[Name = 'a]b']`). Both appear in real projects, and mishandling either
// silently changes the meaning of a query (mendixlabs/mxcli#772). This tracks
// nesting depth and quoting instead.
//
// Returns nil when the input is not a well-formed sequence of bracket groups —
// unbalanced, empty, or with content outside the brackets — so callers can fall
// back to using the string verbatim rather than emit something they invented.
func SplitXPathPredicateGroups(constraint string) []string {
	s := strings.TrimSpace(constraint)
	if s == "" || !strings.HasPrefix(s, "[") {
		return nil
	}

	var groups []string
	var depth int
	var start int
	var inQuote bool

	for i := 0; i < len(s); i++ {
		c := s[i]
		if inQuote {
			// Mendix escapes a quote inside a literal by doubling it.
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inQuote = false
			}
			continue
		}
		switch c {
		case '\'':
			inQuote = true
		case '[':
			if depth == 0 {
				// Anything between groups must be whitespace only.
				if strings.TrimSpace(s[start:i]) != "" {
					return nil
				}
				start = i
			}
			depth++
		case ']':
			depth--
			if depth < 0 {
				return nil
			}
			if depth == 0 {
				groups = append(groups, s[start:i+1])
				start = i + 1
			}
		}
	}

	if depth != 0 || inQuote {
		return nil
	}
	if strings.TrimSpace(s[start:]) != "" {
		return nil
	}
	if len(groups) == 0 {
		return nil
	}
	return groups
}

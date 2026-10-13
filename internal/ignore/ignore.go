// SPDX-License-Identifier: GPL-2.0-or-later

// Package ignore matches paths against .gitignore-style patterns:
//
//   - "*.tmp" matches at any depth; a pattern with a "/" in it ("/build",
//     "docs/drafts") is anchored to the root of the shuttle.
//   - A trailing "/" matches folders only ("node_modules/").
//   - "*" and "?" never match "/"; "[a-z]" and "[!0-9]" are character classes.
//   - "**/" matches any number of folders, "/**" everything inside a folder.
//   - "!" re-includes what an earlier pattern ignored; the last match wins.
//   - "#" starts a comment; "\" escapes the next character ("\#", "\!").
//
// As with git, nothing inside an ignored folder can be re-included: the
// scanner never descends into it.
//
// A Matcher can also leave out what a shuttle does not select: see Select.
//
// This package must stay free of desktop-specific imports; see the
// portability rule in SPEC.md.
package ignore

import (
	"fmt"
	"regexp"
	"strings"
)

type rule struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

// Matcher decides which paths are ignored. The zero value ignores nothing.
type Matcher struct {
	rules   []rule
	include []string
	exclude []string
}

// Compile builds a Matcher from patterns, in order.
func Compile(patterns []string) (*Matcher, error) {
	m := &Matcher{}
	for _, p := range patterns {
		r, ok, err := compile(p)
		if err != nil {
			return nil, fmt.Errorf("ignore pattern %q: %w", p, err)
		}
		if ok {
			m.rules = append(m.rules, r)
		}
	}
	return m, nil
}

// Match reports whether the slash-separated path p is ignored. isDir says
// whether p is a folder.
func (m *Matcher) Match(p string, isDir bool) bool {
	if m.unselected(p, isDir) {
		return true
	}
	ignored := false
	for _, r := range m.rules {
		if r.dirOnly && !isDir {
			continue
		}
		if r.re.MatchString(p) {
			ignored = !r.negate
		}
	}
	return ignored
}

// Select also ignores what a shuttle does not select. include and exclude
// are slash paths, taken literally, not as patterns: with include set,
// only those paths are kept, with what is inside them and the folders that
// lead to them; exclude leaves out those paths and what is inside them.
// Empty lists select everything.
func (m *Matcher) Select(include, exclude []string) {
	m.include, m.exclude = include, exclude
}

func (m *Matcher) unselected(p string, isDir bool) bool {
	for _, x := range m.exclude {
		if within(p, x) {
			return true
		}
	}
	if len(m.include) == 0 {
		return false
	}
	for _, x := range m.include {
		// A folder on the way to an included path is kept, so the scanner
		// goes in; the rest of what is in it is not.
		if within(p, x) || isDir && within(x, p) {
			return false
		}
	}
	return true
}

// within reports whether p is the path top or inside it.
func within(p, top string) bool {
	rest, ok := strings.CutPrefix(p, top)
	return ok && (rest == "" || rest[0] == '/')
}

// compile turns one pattern into a rule; ok is false for blank lines and
// comments.
func compile(p string) (r rule, ok bool, err error) {
	p = strings.TrimSpace(p)
	if p == "" || strings.HasPrefix(p, "#") {
		return rule{}, false, nil
	}
	if rest, found := strings.CutPrefix(p, "!"); found {
		r.negate, p = true, rest
	}
	if rest, found := strings.CutSuffix(p, "/"); found {
		r.dirOnly, p = true, rest
	}
	anchored := strings.Contains(p, "/")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return rule{}, false, fmt.Errorf("matches nothing")
	}

	body, err := translate(p)
	if err != nil {
		return rule{}, false, err
	}
	prefix := "^(?:.*/)?" // any depth
	if anchored {
		prefix = "^"
	}
	r.re, err = regexp.Compile(prefix + body + "$")
	if err != nil {
		return rule{}, false, err
	}
	return r, true, nil
}

// translate turns a glob into the body of a regular expression.
func translate(glob string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch c {
		case '\\':
			if i+1 == len(glob) {
				return "", fmt.Errorf("ends with an unfinished escape")
			}
			i++
			b.WriteString(regexp.QuoteMeta(string(glob[i])))
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				atStart := i == 0 || glob[i-1] == '/'
				switch {
				case atStart && i+2 < len(glob) && glob[i+2] == '/':
					b.WriteString("(?:.*/)?") // "**/": zero or more folders
					i += 2
					continue
				case atStart && i+2 == len(glob):
					b.WriteString(".*") // trailing "/**": everything inside
					i++
					continue
				}
			}
			b.WriteString("[^/]*")
		case '?':
			b.WriteString("[^/]")
		case '[':
			end := classEnd(glob, i)
			if end < 0 {
				return "", fmt.Errorf("unclosed [")
			}
			class := glob[i+1 : end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i = end
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String(), nil
}

// classEnd returns the index of the "]" closing the class opened at i, or -1.
// A "]" right after "[" or "[!" is part of the class, as in shell globs.
func classEnd(glob string, i int) int {
	j := i + 1
	if j < len(glob) && glob[j] == '!' {
		j++
	}
	if j < len(glob) && glob[j] == ']' {
		j++
	}
	for ; j < len(glob); j++ {
		if glob[j] == ']' {
			return j
		}
	}
	return -1
}

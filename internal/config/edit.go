// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// Doc is a config file being edited. Edits touch only the bytes of the keys
// and tables they change: comments, blank lines and the order of everything
// else stay as the user wrote them.
type Doc struct {
	data  []byte
	nl    string // the file's line ending
	exprs []expr // top-level expressions, in order
}

// expr is one top-level line or block of the document.
type expr struct {
	header     bool     // a [table] header; otherwise a key = value
	path       []string // a header's table; a key-value's full dotted path
	keyLen     int      // a key-value's number of key parts
	start, end int      // the whole lines it occupies, newline included
	valueStart int      // a key-value's value...
	valueEnd   int      // ...up to its end, before any trailing comment
}

// Field is one key of an Inline table.
type Field struct {
	Key   string
	Value any
}

// Inline is an inline table value, written in this order:
// { kind = "trash", keep = "30d" }.
type Inline []Field

// ParseDoc reads a config document for editing. It checks only that it is
// TOML; whether it is a valid config is up to Parse.
func ParseDoc(data []byte) (*Doc, error) {
	d := &Doc{data: slices.Clone(data), nl: "\n"}
	if bytes.Contains(data, []byte("\r\n")) {
		d.nl = "\r\n"
	}
	if err := d.index(); err != nil {
		return nil, err
	}
	return d, nil
}

// Bytes returns the document as edited.
func (d *Doc) Bytes() []byte { return slices.Clone(d.data) }

func (d *Doc) index() error {
	var p unstable.Parser
	p.Reset(d.data)
	d.exprs = d.exprs[:0]
	var table []string
	for p.NextExpression() {
		n := p.Expression()
		var parts []string
		var first, last unstable.Range
		for it := n.Key(); it.Next(); {
			k := it.Node()
			if len(parts) == 0 {
				first = k.Raw
			}
			last = k.Raw
			parts = append(parts, string(k.Data))
		}
		e := expr{start: d.lineStart(int(first.Offset))}
		switch n.Kind {
		case unstable.Table, unstable.ArrayTable:
			table = parts
			e.header, e.path = true, parts
			e.end = d.lineEnd(int(last.Offset + last.Length))
		case unstable.KeyValue:
			e.path = append(slices.Clone(table), parts...)
			e.keyLen = len(parts)
			e.valueEnd = int(n.Raw.Offset + n.Raw.Length)
			e.valueStart = int(last.Offset + last.Length)
			e.valueStart += bytes.IndexByte(d.data[e.valueStart:], '=') + 1
			for e.valueStart < e.valueEnd && (d.data[e.valueStart] == ' ' || d.data[e.valueStart] == '\t') {
				e.valueStart++
			}
			e.end = d.lineEnd(e.valueEnd)
		default:
			continue
		}
		d.exprs = append(d.exprs, e)
	}
	return p.Error()
}

// lineStart is the start of the line holding offset i.
func (d *Doc) lineStart(i int) int {
	return bytes.LastIndexByte(d.data[:i], '\n') + 1
}

// lineEnd is just past the newline ending the line holding offset i.
func (d *Doc) lineEnd(i int) int {
	if j := bytes.IndexByte(d.data[i:], '\n'); j >= 0 {
		return i + j + 1
	}
	return len(d.data)
}

// Set sets key in table (e.g. ["shuttles", "docs"], "mode") to v: a string,
// bool, int, []string or Inline. However the key was defined before (a
// plain key, dotted keys, a [table.key] section), it ends up as one
// key = value line, in place of the first definition when there was one.
// The table is created if needed.
func (d *Doc) Set(table []string, key string, v any) error {
	if len(table) == 0 {
		return errors.New("config keys live in tables")
	}
	val, err := encodeValue(v)
	if err != nil {
		return fmt.Errorf("%s.%s: %w", strings.Join(table, "."), key, err)
	}
	if err := d.unInline(table); err != nil {
		return err
	}
	full := append(slices.Clone(table), key)

	// A plain "key = value" already in place: change only the value, so a
	// comment after it on the line survives.
	defs := d.definitions(full)
	if len(defs) == 1 && !defs[0].header && slices.Equal(defs[0].path, full) {
		e := defs[0]
		return d.splice(e.valueStart, e.valueEnd, val)
	}

	// Otherwise remove every definition and write one line where the first
	// key-value outside a removed section was, or where the table's own
	// keys end.
	for _, e := range defs {
		if section := len(e.path) - e.keyLen; !e.header && section < len(full) {
			// Written relative to the section that line is in, which may be
			// an outer one holding dotted keys.
			return d.replaceAll(defs, e.start, encodeKeys(full[section:])+" = "+val+d.nl)
		}
	}
	if err := d.replaceAll(defs, -1, ""); err != nil {
		return err
	}
	at, prefix, ok := d.insertPoint(table, key)
	if !ok {
		return d.appendTable(table, encodeKey(key)+" = "+val)
	}
	line := prefix + " = " + val + d.nl
	if at > 0 && d.data[at-1] != '\n' { // the last line, with no newline
		line = d.nl + line
	}
	return d.splice(at, at, line)
}

// Delete removes key from table, however it was defined. Removing a key
// that is not there is not an error.
func (d *Doc) Delete(table []string, key string) error {
	if err := d.unInline(table); err != nil {
		return err
	}
	return d.replaceAll(d.definitions(append(slices.Clone(table), key)), -1, "")
}

// DeleteTable removes a table with everything in and below it, including
// the comment lines directly above its header.
func (d *Doc) DeleteTable(table []string) error {
	if len(table) == 0 {
		return errors.New("cannot delete the whole document")
	}
	// Only a table around it needs taking apart: a value at its own path
	// is simply one of its definitions.
	if err := d.unInline(table[:len(table)-1]); err != nil {
		return err
	}
	return d.replaceAll(d.definitions(table), -1, "")
}

// HasTable reports whether the table is defined.
func (d *Doc) HasTable(table []string) bool { return len(d.definitions(table)) > 0 }

// definitions are the expressions defining path: key-values at or below it,
// and headers at or below it, each header with its whole section.
func (d *Doc) definitions(path []string) []expr {
	var out []expr
	for i, e := range d.exprs {
		if !hasPrefix(e.path, path) {
			continue
		}
		if e.header {
			e.start = d.commentsAbove(e.start)
			e.end = d.sectionEnd(i)
		}
		out = append(out, e)
	}
	return out
}

// sectionEnd is where the section of header i ends: at the next header, but
// before the comments that belong to that header.
func (d *Doc) sectionEnd(i int) int {
	for _, e := range d.exprs[i+1:] {
		if e.header {
			return d.commentsAbove(e.start)
		}
	}
	return len(d.data)
}

// commentsAbove extends start over the comment lines directly above it, with
// no blank line in between: they describe what follows.
func (d *Doc) commentsAbove(start int) int {
	for start > 0 {
		prev := d.lineStart(start - 1)
		if !bytes.HasPrefix(bytes.TrimLeft(d.data[prev:start], " \t"), []byte("#")) {
			break
		}
		start = prev
	}
	return start
}

// insertPoint is where a new key of table goes, and how it is written
// there: after the table's own last key-value, as a plain key under its
// header, or as a dotted key next to the dotted keys defining the table. ok
// is false when the table is not defined at all.
func (d *Doc) insertPoint(table []string, key string) (at int, line string, ok bool) {
	section := []string(nil) // the header the expressions so far fall under
	found := false
	for _, e := range d.exprs {
		if e.header {
			section = e.path
			if slices.Equal(e.path, table) {
				at, line, found = e.end, encodeKey(key), true
			}
			continue
		}
		switch {
		case slices.Equal(section, table):
			at = e.end // the table's own keys
		case !found && hasPrefix(e.path, table) && len(section) < len(table):
			// Dotted keys from an outer section define it: add one more.
			at, line, ok = e.end, encodeKeys(append(slices.Clone(table[len(section):]), key)), true
		}
	}
	return at, line, found || ok
}

// appendTable adds a [table] header with one line of content, after the
// last table sharing its parent, or at the end.
func (d *Doc) appendTable(table []string, line string) error {
	at := len(d.data)
	parent := table[:len(table)-1]
	for i, e := range d.exprs {
		if e.header && len(parent) > 0 && hasPrefix(e.path, parent) {
			at = d.sectionEnd(i)
		}
	}
	text := "[" + encodeKeys(table) + "]" + d.nl + line + d.nl
	// One blank line before the header, and one after it if a table follows.
	before := d.data[:at]
	switch {
	case len(before) == 0:
	case !bytes.HasSuffix(before, []byte("\n")):
		text = d.nl + d.nl + text
	case !bytes.HasSuffix(bytes.TrimRight(before, "\r"), []byte("\n\n")) && !bytes.HasSuffix(before, []byte("\n\r\n")):
		text = d.nl + text
	}
	if at < len(d.data) {
		text += d.nl
	}
	return d.splice(at, at, text)
}

// unInline turns the inline table (or dotted-key value) holding table, or
// any of its parents, into a [header] section at the end of the document,
// so its keys can be edited one by one. Comments inside that inline table
// are lost; values are kept.
func (d *Doc) unInline(table []string) error {
	for {
		var holder *expr
		for i := range d.exprs {
			e := &d.exprs[i]
			if !e.header && len(e.path) <= len(table) && hasPrefix(table, e.path) {
				holder = e
				break
			}
		}
		if holder == nil {
			return nil
		}
		var v struct{ V map[string]any }
		if err := toml.Unmarshal(append([]byte("V = "), d.data[holder.valueStart:holder.valueEnd]...), &v); err != nil {
			return fmt.Errorf("%s is not a table", strings.Join(holder.path, "."))
		}
		var lines []string
		for _, k := range slices.Sorted(maps.Keys(v.V)) {
			val, err := encodeValue(v.V[k])
			if err != nil {
				return err
			}
			lines = append(lines, encodeKey(k)+" = "+val)
		}
		path := holder.path
		remove := []expr{*holder}
		// The section it leaves, when nothing else is in it: [shuttles]
		// above "docs = { ... }" would stay behind empty.
		for i, e := range d.exprs {
			if e.header && slices.Equal(e.path, path[:len(path)-holder.keyLen]) {
				if d.onlyBlank(e.end, d.sectionEnd(i), holder) {
					e.end = d.sectionEnd(i)
					remove = append(remove, e)
				}
			}
		}
		if err := d.replaceAll(remove, -1, ""); err != nil {
			return err
		}
		end := len(d.data)
		text := "[" + encodeKeys(path) + "]" + d.nl + strings.Join(lines, d.nl)
		if len(lines) > 0 {
			text += d.nl
		}
		if end > 0 && !bytes.HasSuffix(d.data, []byte("\n")) {
			text = d.nl + text
		}
		if end > 0 {
			text = d.nl + text
		}
		if err := d.splice(end, end, text); err != nil {
			return err
		}
	}
}

// replaceAll removes the given expressions and inserts text at offset at
// (an offset in the document before the removal; -1 for none), then
// re-reads the document.
func (d *Doc) replaceAll(remove []expr, at int, text string) error {
	type cut struct{ start, end int }
	var cuts []cut
	for _, e := range remove {
		// Between two blank lines, take the blank lines after it too, so the
		// cut does not leave a gap of several.
		start, end := e.start, e.end
		if start == 0 || d.blankLine(d.lineStart(start-1)) {
			for end < len(d.data) && d.blankLine(end) {
				end = d.lineEnd(end)
			}
		}
		if end == len(d.data) { // nor blank lines at the end
			for start > 0 && d.blankLine(d.lineStart(start-1)) {
				start = d.lineStart(start - 1)
			}
		}
		cuts = append(cuts, cut{start, end})
	}
	slices.SortFunc(cuts, func(a, b cut) int { return a.start - b.start })
	var out []byte
	pos := 0
	inserted := at < 0
	for _, c := range cuts {
		if c.start < pos { // inside a section already cut
			pos = max(pos, c.end)
			continue
		}
		if !inserted && at <= c.start {
			out = append(out, d.data[pos:at]...)
			out = append(out, text...)
			pos, inserted = at, true
		}
		out = append(out, d.data[pos:c.start]...)
		pos = c.end
	}
	if !inserted {
		at = max(at, pos)
		out = append(out, d.data[pos:at]...)
		out = append(out, text...)
		pos = at
	}
	out = append(out, d.data[pos:]...)
	return d.set(out)
}

// onlyBlank reports whether data[start:end] holds nothing but blank lines
// and the expression except.
func (d *Doc) onlyBlank(start, end int, except *expr) bool {
	for i := start; i < end; i = d.lineEnd(i) {
		if i >= except.start && i < except.end {
			continue
		}
		if !d.blankLine(i) {
			return false
		}
	}
	return true
}

// blankLine reports whether the line starting at i is empty or whitespace.
func (d *Doc) blankLine(i int) bool {
	return len(bytes.TrimSpace(d.data[i:d.lineEnd(i)])) == 0
}

func (d *Doc) splice(start, end int, text string) error {
	out := slices.Concat(d.data[:start], []byte(text), d.data[end:])
	return d.set(out)
}

func (d *Doc) set(data []byte) error {
	old := d.data
	d.data = data
	if err := d.index(); err != nil {
		d.data = old
		_ = d.index()
		return fmt.Errorf("editing the config produced invalid TOML (a bug in shuttle): %w", err)
	}
	return nil
}

func hasPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

// encodeKey writes one key part, bare when TOML allows it.
func encodeKey(k string) string {
	bare := k != ""
	for _, c := range []byte(k) {
		bare = bare && (c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_')
	}
	if bare {
		return k
	}
	return encodeString(k)
}

func encodeKeys(parts []string) string {
	enc := make([]string, len(parts))
	for i, p := range parts {
		enc[i] = encodeKey(p)
	}
	return strings.Join(enc, ".")
}

// encodeString writes a TOML basic string.
func encodeString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func encodeValue(v any) (string, error) {
	switch v := v.(type) {
	case string:
		return encodeString(v), nil
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64), nil
	case []string:
		return encodeList(len(v), func(i int) (string, error) { return encodeString(v[i]), nil })
	case []any:
		return encodeList(len(v), func(i int) (string, error) { return encodeValue(v[i]) })
	case Inline:
		parts := make([]string, 0, len(v))
		for _, f := range v {
			val, err := encodeValue(f.Value)
			if err != nil {
				return "", err
			}
			parts = append(parts, encodeKey(f.Key)+" = "+val)
		}
		if len(parts) == 0 {
			return "{}", nil
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	case map[string]any:
		in := make(Inline, 0, len(v))
		for _, k := range slices.Sorted(maps.Keys(v)) {
			in = append(in, Field{k, v[k]})
		}
		return encodeValue(in)
	case fmt.Stringer: // local dates and times, as decoded
		return v.String(), nil
	}
	return "", fmt.Errorf("cannot write a %T", v)
}

func encodeList(n int, item func(int) (string, error)) (string, error) {
	parts := make([]string, n)
	for i := range n {
		s, err := item(i)
		if err != nil {
			return "", err
		}
		parts[i] = s
	}
	return "[" + strings.Join(parts, ", ") + "]", nil
}

// ErrChanged means the config file changed on disk after it was read, by
// another editor or the daemon: saving would undo that change.
var ErrChanged = errors.New("the config file changed since it was read; reload it and try again")

// Save writes after to path, if the file still holds before (nil: if it
// does not exist yet). after must be a valid config: home is for Parse. The
// file is replaced atomically, keeping its permissions.
func Save(path string, before, after []byte, home string) error {
	cur, err := os.ReadFile(path) //nolint:gosec // the user's own config file
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if before != nil {
			return ErrChanged
		}
	case err != nil:
		return err
	case before == nil || !bytes.Equal(cur, before):
		return ErrChanged
	}
	if _, err := Parse(after, home); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	mode := fs.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	f, err := os.CreateTemp(dir, ".config.toml.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // gone after the rename; cleans up after a failure
	if _, err := f.Write(after); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

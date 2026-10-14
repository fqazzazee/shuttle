// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"cmp"
	"context"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// picking is what a shuttle syncs of its folder.
type picking int

const (
	pickAll     picking = iota // everything
	pickInclude                // only what is marked: the shuttle's include
	pickExclude                // all but what is marked: its exclude
)

var pickingNames = []string{"everything", "only what is marked", "all but what is marked"}

// entry is one thing in a folder.
type entry struct {
	name string
	dir  bool
}

// node is a file or folder in the tree; path is a slash path from the
// share's top, or this computer's, "/" for the top itself.
type node struct {
	path    string
	dir     bool
	open    bool
	loaded  bool
	loading bool
	err     error
	kids    []*node
}

func (n *node) name() string { return path.Base(n.path) }

// tree picks the shuttle's folder on the share or on this computer, shown
// with what is in it, folders opening in place, and marks the files and
// folders it syncs or leaves out.
type tree struct {
	id    int
	where string // "on nas", "here"
	list  func(ctx context.Context, dir string) ([]entry, error)
	clean func(typed string) string // a typed path as the tree's
	root  *node                     // the shuttle's folder
	sel   int                       // in rows()
	pick  picking
	marks map[string]bool // slash paths from the top
	// folders are marks the other side's tree showed as folders: not
	// here, they would not be known to be.
	folders map[string]bool
	typing  bool
	input   *textinput.Model
	note    string
}

var treeIDs int

func cleanSlash(p string) string {
	return path.Clean("/" + strings.TrimPrefix(p, "/"))
}

type treeListedMsg struct {
	id      int
	dir     string
	entries []entry
	err     error
}

// remoteTree shows the share through f, from dir.
func remoteTree(where string, f fsys.FS, dir string) *tree {
	return newTree(where, dir, func(ctx context.Context, dir string) ([]entry, error) {
		return listFS(ctx, f, dir)
	})
}

// localTree shows this computer, from dir. A typed path may start with ~,
// for home.
func localTree(dir, home string) *tree {
	t := newTree("here", dir, func(ctx context.Context, dir string) ([]entry, error) {
		// Through the local backend, as a run sees it; opened for each
		// listing, so there is nothing to close.
		f, err := fsys.OpenLocal("/")
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return listFS(ctx, f, dir)
	})
	t.clean = func(p string) string {
		if rest, ok := strings.CutPrefix(p, "~"); ok {
			p = filepath.Join(home, rest)
		}
		if !filepath.IsAbs(p) {
			return p // the wizard refuses it, with an example
		}
		return filepath.Clean(p)
	}
	return t
}

// listFS lists dir, a slash path from f's top, as the tree shows it.
func listFS(ctx context.Context, f fsys.FS, dir string) ([]entry, error) {
	rel := strings.TrimPrefix(dir, "/")
	if rel == "" {
		rel = "."
	}
	infos, err := f.ReadDir(ctx, rel)
	if err != nil {
		return nil, err
	}
	var out []entry
	for _, fi := range infos {
		base := path.Base(fi.Name)
		// Never synced, so never marked: the versions folder, temp files
		// of a run in progress, and what is neither file nor folder.
		if base == engine.VersionsDir || fsys.IsTemp(base) || fi.Kind != fsys.KindDir && fi.Kind != fsys.KindFile {
			continue
		}
		out = append(out, entry{name: base, dir: fi.Kind == fsys.KindDir})
	}
	return out, nil
}

func newTree(where, dir string, list func(context.Context, string) ([]entry, error)) *tree {
	treeIDs++
	dir = cleanSlash(dir)
	return &tree{id: treeIDs, where: where, list: list, clean: cleanSlash,
		root: &node{path: dir, dir: true, open: true}, marks: map[string]bool{}, input: newInput(dir)}
}

// dir is the shuttle's folder: the top of the tree.
func (t *tree) dir() string { return t.root.path }

// load lists the folder n.
func (t *tree) load(ctx context.Context, n *node) tea.Cmd {
	n.loading, n.err = true, nil
	id, dir, list := t.id, n.path, t.list
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, keyringTimeout)
		defer cancel()
		entries, err := list(ctx, dir)
		// Folders first, as in a file manager, and hidden ones after the
		// rest: a home folder has many.
		slices.SortFunc(entries, func(a, b entry) int {
			if a.dir != b.dir {
				if a.dir {
					return -1
				}
				return 1
			}
			if ha, hb := strings.HasPrefix(a.name, "."), strings.HasPrefix(b.name, "."); ha != hb {
				if hb {
					return -1
				}
				return 1
			}
			return cmp.Compare(a.name, b.name)
		})
		return treeListedMsg{id: id, dir: dir, entries: entries, err: err}
	}
}

// find is the node at p, if it is in the tree.
func (t *tree) find(p string) *node {
	n := t.root
	if p == n.path {
		return n
	}
	rest, ok := strings.CutPrefix(p, strings.TrimSuffix(n.path, "/")+"/")
	if !ok {
		return nil
	}
	for name := range strings.SplitSeq(rest, "/") {
		i := slices.IndexFunc(n.kids, func(k *node) bool { return k.name() == name })
		if i < 0 {
			return nil
		}
		n = n.kids[i]
	}
	return n
}

func (t *tree) listed(msg treeListedMsg) {
	if msg.id != t.id {
		return
	}
	n := t.find(msg.dir)
	if n == nil {
		return // a folder no longer shown
	}
	selPath := ""
	if rows := t.rows(); t.sel < len(rows) {
		selPath = rows[t.sel].path
	}
	n.loading, n.err = false, msg.err
	if msg.err == nil {
		// Listed again: what was there keeps its own state.
		kids := make([]*node, 0, len(msg.entries))
		for _, e := range msg.entries {
			p := path.Join(n.path, e.name)
			if i := slices.IndexFunc(n.kids, func(k *node) bool { return k.path == p && k.dir == e.dir }); i >= 0 {
				kids = append(kids, n.kids[i])
				continue
			}
			kids = append(kids, &node{path: p, dir: e.dir})
		}
		n.kids, n.loaded = kids, true
	}
	t.selectPath(selPath)
}

// rows are the nodes shown, in order: what is in the open folders.
func (t *tree) rows() []*node {
	var out []*node
	var walk func(n *node)
	walk = func(n *node) {
		for _, k := range n.kids {
			out = append(out, k)
			if k.open {
				walk(k)
			}
		}
	}
	walk(t.root)
	return out
}

func (t *tree) selectPath(p string) {
	rows := t.rows()
	if i := slices.IndexFunc(rows, func(n *node) bool { return n.path == p }); i >= 0 {
		t.sel = i
	}
	t.sel = min(t.sel, max(len(rows)-1, 0))
}

func (t *tree) current() *node {
	if rows := t.rows(); t.sel < len(rows) {
		return rows[t.sel]
	}
	return nil
}

// parentOf is the folder p is in.
func parentOf(p string) string { return path.Dir(p) }

// key handles a key; chosen reports that the folder shown is chosen.
func (t *tree) key(ctx context.Context, msg tea.KeyPressMsg) (cmd tea.Cmd, chosen bool) {
	k := msg.String()
	t.note = ""
	if t.typing {
		switch k {
		case "enter":
			t.typing = false
			t.setRoot(&node{path: t.clean(strings.TrimSpace(t.input.Value())), dir: true})
			t.sel = 0
			// Listed for when the user comes back to this step.
			return t.load(ctx, t.root), true
		case "esc":
			t.typing = false
			return nil, false
		}
		updated, cmd := t.input.Update(msg)
		*t.input = updated
		return cmd, false
	}
	n, rows := t.current(), t.rows()
	switch k {
	case "up", "k":
		t.sel = max(t.sel-1, 0)
	case "down", "j":
		t.sel = min(t.sel+1, max(len(rows)-1, 0))
	case "home", "g":
		t.sel = 0
	case "end", "G":
		t.sel = max(len(rows)-1, 0)
	case "right", "l":
		switch {
		case n == nil || !n.dir:
		case !n.open:
			// Listed every time it opens: the tree shows the share as it is.
			n.open = true
			return t.load(ctx, n), false
		case len(n.kids) > 0:
			t.sel++
		}
	case "left", "h":
		switch {
		case n != nil && n.open:
			n.open = false
		case n != nil && parentOf(n.path) != t.root.path:
			t.selectPath(parentOf(n.path))
		default:
			return t.up(ctx), false
		}
	case "backspace":
		return t.up(ctx), false
	case "o":
		if n != nil && n.dir {
			n.open = true
			t.setRoot(n)
			t.sel = 0
			return t.load(ctx, n), false
		}
	case "m":
		t.pick = (t.pick + 1) % picking(len(pickingNames))
	case "space":
		switch {
		case n == nil:
		case t.pick == pickAll:
			t.note = "everything is synced: m chooses to mark what to sync, or what to leave out"
		default:
			t.toggle(n)
		}
	case "enter":
		return nil, true
	case "t", "/":
		t.typing = true
		t.input.SetValue(t.root.path)
		t.input.CursorEnd()
		return t.input.Focus(), false
	}
	return nil, false
}

func (t *tree) setRoot(n *node) {
	t.root = n
	t.root.open = true
}

// up makes the folder above the shuttle's folder, with the folder it was
// open inside it.
func (t *tree) up(ctx context.Context) tea.Cmd {
	if t.root.path == "/" {
		return nil
	}
	old := t.root
	t.setRoot(&node{path: parentOf(old.path), dir: true, kids: []*node{old}})
	t.sel = 0
	return t.load(ctx, t.root)
}

// marked reports whether p or a folder it is in is marked, inside the
// shuttle's folder: marks outside it are not saved, so they do not count.
func (t *tree) marked(p string) bool {
	for ; within(p, t.root.path) && p != t.root.path; p = parentOf(p) {
		if t.marks[p] {
			return true
		}
	}
	return false
}

// partly reports whether something inside p is marked.
func (t *tree) partly(p string) bool {
	for m := range t.marks {
		if within(m, p) && m != p {
			return true
		}
	}
	return false
}

// synced reports whether the shuttle syncs p: all of it, some of it.
func (t *tree) synced(p string) (all, some bool) {
	switch {
	case t.pick == pickAll:
		return true, true
	case t.marked(p):
		return t.pick == pickInclude, t.pick == pickInclude
	case t.partly(p):
		return false, true
	}
	return t.pick == pickExclude, t.pick == pickExclude
}

// toggle marks n, or unmarks it. Unmarking something in a marked folder
// unmarks the folder and marks what else is in it instead, down to n.
func (t *tree) toggle(n *node) {
	if !t.marked(n.path) {
		for m := range t.marks {
			if within(m, n.path) {
				delete(t.marks, m)
			}
		}
		t.marks[n.path] = true
		return
	}
	p := n.path
	for !t.marks[p] {
		parent := parentOf(p)
		for _, k := range t.find(parent).kids {
			if k.path != p {
				t.marks[k.path] = true
			}
		}
		p = parent
	}
	delete(t.marks, p)
}

// within reports whether the slash path p is top or inside it.
func within(p, top string) bool {
	if top == "/" {
		return true
	}
	rest, ok := strings.CutPrefix(p, top)
	return ok && (rest == "" || rest[0] == '/')
}

// selection is what the shuttle includes or excludes: the marks inside its
// folder, relative to it, sorted. Marks outside it are left out.
func (t *tree) selection() (include, exclude []string) {
	var rel []string
	for _, m := range t.inside() {
		rel = append(rel, strings.TrimPrefix(strings.TrimPrefix(m, t.root.path), "/"))
	}
	switch t.pick {
	case pickInclude:
		return rel, nil
	case pickExclude:
		return nil, rel
	}
	return nil, nil
}

// inside is the marks inside the shuttle's folder, sorted.
func (t *tree) inside() []string {
	var out []string
	for m := range t.marks {
		if within(m, t.root.path) && m != t.root.path {
			out = append(out, m)
		}
	}
	slices.Sort(out)
	return out
}

// adopt takes from's marks inside its folder, as marks inside this tree's:
// the shuttle has one list of them for both sides.
func (t *tree) adopt(from *tree) {
	t.pick, t.marks, t.folders = from.pick, map[string]bool{}, map[string]bool{}
	for _, m := range from.inside() {
		p := path.Join(t.root.path, strings.TrimPrefix(m, from.root.path))
		t.marks[p] = true
		if from.folder(m) {
			t.folders[p] = true
		}
	}
}

// folder reports whether the mark p is a folder, as far as is known.
func (t *tree) folder(p string) bool {
	if n := t.find(p); n != nil {
		return n.dir
	}
	return t.folders[p]
}

// setSelection marks what the shuttle includes or excludes, as in the config.
func (t *tree) setSelection(include, exclude []string) {
	t.pick = pickAll
	marks := include
	switch {
	case len(include) > 0:
		t.pick = pickInclude
	case len(exclude) > 0:
		t.pick, marks = pickExclude, exclude
	}
	for _, m := range marks {
		t.marks[path.Join(t.root.path, m)] = true
	}
}

func (t *tree) lines(room, width int) []string {
	lines := []string{"Folder " + t.where + ": " + headStyle.Render(t.root.path)}
	choices := make([]string, len(pickingNames))
	for i, name := range pickingNames {
		choices[i] = faint.Render(name)
		if picking(i) == t.pick {
			choices[i] = activeTab.Render(" " + name + " ")
		}
	}
	lines = append(lines, "Sync: "+strings.Join(choices, "  "))
	if t.pick != pickAll {
		lines = append(lines, t.marksLine(width))
	}
	lines = append(lines, "")
	r := t.root
	switch {
	case t.typing:
		return append(lines, "Path: "+t.input.View())
	case r.err != nil:
		return append(lines, badStyle.Render(firstLine(r.err.Error())), "", "t types the path instead.")
	case !r.loaded:
		return append(lines, faint.Render("Looking inside…"))
	case len(r.kids) == 0:
		return append(lines, faint.Render("Nothing inside: enter uses this folder."))
	}

	// Each row, and under an open folder what it says while it is listed,
	// or that it cannot be.
	var body []string
	at, cur := 0, t.current()
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		for _, k := range n.kids {
			if k == cur {
				at = len(body)
			}
			body = append(body, t.row(k, depth, k == cur))
			if !k.open {
				continue
			}
			indent := strings.Repeat("  ", depth+1) + "    "
			switch {
			case k.err != nil:
				body = append(body, indent+badStyle.Render(firstLine(k.err.Error())))
			case k.loading && !k.loaded:
				body = append(body, indent+faint.Render("looking inside…"))
			case k.loaded && len(k.kids) == 0:
				body = append(body, indent+faint.Render("empty"))
			}
			walk(k, depth+1)
		}
	}
	walk(r, 0)
	if t.note != "" {
		room--
	}
	rows := max(room-len(lines), 1)
	first := min(max(at-rows/2, 0), max(len(body)-rows, 0))
	lines = append(lines, body[first:min(first+rows, len(body))]...)
	if t.note != "" {
		lines = append(lines, heldStyle.Render(t.note))
	}
	return lines
}

func (t *tree) row(n *node, depth int, sel bool) string {
	arrow := "  "
	if n.dir {
		arrow = "▸ "
		if n.open {
			arrow = "▾ "
		}
	}
	name := n.name()
	if n.dir {
		name += "/"
	}
	box := ""
	all, some := t.synced(n.path)
	if t.pick != pickAll {
		switch {
		case all:
			box = "[✓] "
		case some:
			box = "[~] "
		default:
			box = "[ ] "
		}
	}
	line := strings.Repeat("  ", depth) + arrow + box + name
	switch {
	case sel:
		return "› " + selected.Render(line)
	case !all && !some:
		return "  " + faint.Render(line)
	}
	return "  " + line
}

// marksLine lists the marks inside the shuttle's folder.
func (t *tree) marksLine(width int) string {
	inside := t.inside()
	if len(inside) == 0 {
		if t.pick == pickInclude {
			return heldStyle.Render("Nothing marked yet: space marks what to sync.")
		}
		return faint.Render("Nothing marked yet: space marks what to leave out.")
	}
	label := "Synced only: "
	if t.pick == pickExclude {
		label = "Left out: "
	}
	names := make([]string, len(inside))
	for i, m := range inside {
		names[i] = strings.TrimPrefix(strings.TrimPrefix(m, t.root.path), "/")
		if t.folder(m) {
			names[i] += "/"
		}
	}
	return ansi.Truncate(label+strings.Join(names, ", "), max(width-1, 20), "…")
}

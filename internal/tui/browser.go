// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/engine"
	"github.com/fqazzazee/shuttle/internal/fsys"
)

// browser picks a folder, on a share or on this computer, by going in and
// out of folders, or by typing its path.
type browser struct {
	id       int
	where    string // "on nas", "here"
	list     func(ctx context.Context, dir string) ([]string, error)
	join     func(dir, name string) string
	parent   func(dir string) string
	root     string // the top: nothing above it
	dir      string // the folder shown, and chosen with enter
	entries  []string
	sel      int
	err      error
	loading  bool
	typing   bool
	input    *textinput.Model
	cameFrom string // the folder just left, selected in its parent
}

var browserIDs int

type listedMsg struct {
	id      int
	dir     string
	entries []string
	err     error
}

// remoteBrowser browses the folders of a share through f; dir is a slash
// path from the share's top, "/" for the top itself.
func remoteBrowser(where string, f fsys.FS, dir string) *browser {
	return newBrowser(where, "/", cleanSlash(dir), func(ctx context.Context, dir string) ([]string, error) {
		rel := strings.TrimPrefix(dir, "/")
		if rel == "" {
			rel = "."
		}
		infos, err := f.ReadDir(ctx, rel)
		if err != nil {
			return nil, err
		}
		var names []string
		for _, fi := range infos {
			base := path.Base(fi.Name)
			if fi.Kind == fsys.KindDir && base != engine.VersionsDir {
				names = append(names, base)
			}
		}
		return names, nil
	}, func(dir, name string) string { return path.Join(dir, name) }, path.Dir)
}

func cleanSlash(p string) string {
	return path.Clean("/" + strings.TrimPrefix(p, "/"))
}

// localBrowser browses this computer's folders, the hidden ones left out.
func localBrowser(dir string) *browser {
	return newBrowser("here", "/", filepath.Clean(dir), func(_ context.Context, dir string) ([]string, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		var names []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil && info.IsDir() {
				names = append(names, e.Name())
			}
		}
		return names, nil
	}, func(dir, name string) string { return filepath.Join(dir, name) }, filepath.Dir)
}

func newBrowser(where, root, dir string, list func(context.Context, string) ([]string, error),
	join func(string, string) string, parent func(string) string) *browser {
	browserIDs++
	return &browser{id: browserIDs, where: where, root: root, dir: dir, list: list, join: join, parent: parent,
		input: newInput(dir)}
}

// load lists the folder shown.
func (b *browser) load(ctx context.Context) tea.Cmd {
	b.loading, b.err = true, nil
	id, dir, list := b.id, b.dir, b.list
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, keyringTimeout)
		defer cancel()
		entries, err := list(ctx, dir)
		slices.Sort(entries)
		return listedMsg{id: id, dir: dir, entries: entries, err: err}
	}
}

func (b *browser) listed(msg listedMsg) {
	if msg.id != b.id || msg.dir != b.dir {
		return // for a folder no longer shown
	}
	b.loading, b.entries, b.err, b.sel = false, msg.entries, msg.err, 0
	if i := slices.Index(b.entries, b.cameFrom); i >= 0 {
		b.sel = i
	}
}

// key handles a key; chosen reports that the folder shown is chosen.
func (b *browser) key(ctx context.Context, msg tea.KeyPressMsg) (cmd tea.Cmd, chosen bool) {
	k := msg.String()
	if b.typing {
		switch k {
		case "enter":
			b.typing = false
			b.dir = strings.TrimSpace(b.input.Value())
			return nil, true
		case "esc":
			b.typing = false
			return nil, false
		}
		updated, cmd := b.input.Update(msg)
		*b.input = updated
		return cmd, false
	}
	switch k {
	case "up", "k":
		b.sel = max(b.sel-1, 0)
	case "down", "j":
		b.sel = min(b.sel+1, max(len(b.entries)-1, 0))
	case "home", "g":
		b.sel = 0
	case "end", "G":
		b.sel = max(len(b.entries)-1, 0)
	case "right", "l":
		if b.sel < len(b.entries) && !b.loading {
			b.dir, b.cameFrom = b.join(b.dir, b.entries[b.sel]), ""
			return b.load(ctx), false
		}
	case "left", "h", "backspace":
		if b.dir != b.root && !b.loading {
			b.dir, b.cameFrom = b.parent(b.dir), path.Base(filepath.ToSlash(b.dir))
			return b.load(ctx), false
		}
	case "enter":
		return nil, true
	case "t", "/":
		b.typing = true
		b.input.SetValue(b.dir)
		b.input.CursorEnd()
		return b.input.Focus(), false
	}
	return nil, false
}

func (b *browser) lines(room int) []string {
	lines := []string{"Folder " + b.where + ": " + headStyle.Render(b.dir), ""}
	switch {
	case b.typing:
		return append(lines, "Path: "+b.input.View())
	case b.loading:
		return append(lines, faint.Render("Looking inside…"))
	case b.err != nil:
		return append(lines, badStyle.Render(firstLine(b.err.Error())), "", "t types the path instead.")
	case len(b.entries) == 0:
		return append(lines, faint.Render("No folders inside: enter uses this one."))
	}
	rows := max(room-len(lines), 1)
	first := min(max(b.sel-rows/2, 0), max(len(b.entries)-rows, 0))
	for i := first; i < len(b.entries) && i < first+rows; i++ {
		line := "  " + b.entries[i] + "/"
		if i == b.sel {
			line = selected.Render("▸ " + b.entries[i] + "/")
		}
		lines = append(lines, line)
	}
	return lines
}

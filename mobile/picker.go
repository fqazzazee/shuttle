// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package mobile

import (
	"encoding/json"
	"path"
	"strings"
	"sync"

	"github.com/fqazzazee/shuttle/internal/pick"
	"github.com/fqazzazee/shuttle/internal/shares"
)

// What a Picker's marks mean (pick.Mode).
const (
	PickAll     = int(pick.All)     // everything is synced
	PickInclude = int(pick.Include) // only what is marked
	PickExclude = int(pick.Exclude) // all but what is marked
)

// How much of a file or folder a Picker's marks sync (Synced).
const (
	SyncedNone = 0
	SyncedSome = 1 // a folder with something inside marked
	SyncedAll  = 2
)

// Picker is what the wizard marks in the shuttle's folder (pick.Marks),
// as the TUI's tree marks it. The app keeps one for the wizard and tells it
// what each folder holds as it lists it (Listed): unmarking something in a
// marked folder marks the rest of it instead.
type Picker struct {
	mu    sync.Mutex
	root  string
	marks pick.Marks
	kids  map[string][]string // slash paths, by the folder they are in
	dirs  map[string]bool     // the listed paths that are folders
}

// NewPicker is a Picker for a shuttle on root, a slash path from the top of
// the share, that syncs everything.
func NewPicker(root string) *Picker {
	return &Picker{root: root, kids: map[string][]string{}, dirs: map[string]bool{}}
}

// SetRoot moves the shuttle's folder: the marks outside it are kept, for
// coming back, and do not count meanwhile.
func (p *Picker) SetRoot(root string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.root = root
}

// Root is the shuttle's folder.
func (p *Picker) Root() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.root
}

// Mode is what the marks mean: PickAll, PickInclude or PickExclude.
func (p *Picker) Mode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return int(p.marks.Mode)
}

// SetMode says what the marks mean; they are kept across modes.
func (p *Picker) SetMode(mode int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.marks.Mode = pick.Mode(mode)
}

// Listed tells the picker what the folder dir holds, as BrowseJSON listed it.
func (p *Picker) Listed(dir, listingJSON string) error {
	var list []shares.Entry
	if err := json.Unmarshal([]byte(listingJSON), &list); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	kids := make([]string, len(list))
	for i, e := range list {
		kids[i] = path.Join(dir, e.Name)
		p.dirs[kids[i]] = e.Dir
	}
	p.kids[dir] = kids
	return nil
}

// Toggle marks the file or folder at the slash path, or unmarks it.
func (p *Picker) Toggle(at string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.marks.Toggle(at, p.root, func(dir string) []string { return p.kids[dir] })
}

// Synced is how much of the file or folder at the slash path the shuttle
// syncs: SyncedAll, SyncedSome or SyncedNone.
func (p *Picker) Synced(at string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch all, some := p.marks.Synced(at, p.root); {
	case all:
		return SyncedAll
	case some:
		return SyncedSome
	}
	return SyncedNone
}

// MarksJSON is the marks inside the shuttle's folder, relative to it and
// sorted, folders ending in "/" where listed as such: ["Photos/", "a.txt"].
func (p *Picker) MarksJSON() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []string{}
	for _, m := range p.marks.Inside(p.root) {
		rel := strings.TrimPrefix(strings.TrimPrefix(m, p.root), "/")
		if p.dirs[m] {
			rel += "/"
		}
		out = append(out, rel)
	}
	return toJSON(out)
}

// SelectionJSON is what the shuttle includes or excludes, as the config
// takes it: {"Include": ["Photos"], "Exclude": null}.
func (p *Picker) SelectionJSON() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	include, exclude := p.marks.Selection(p.root)
	return toJSON(struct{ Include, Exclude []string }{include, exclude})
}

// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/secrets"
)

// wizardSetup is a home with Documents in it, a mounted share with docs
// and music, and a config with one shuttle, docs.
type wizardSetup struct {
	m           *model
	home, share string
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func newWizardSetup(t *testing.T, browseErr error) *wizardSetup {
	t.Helper()
	o := testOptions(t)
	w := &wizardSetup{home: o.Home, share: t.TempDir()}
	mkdirs(t, filepath.Join(w.home, "Documents"), filepath.Join(w.home, "Papers"), filepath.Join(w.home, ".hidden"),
		filepath.Join(w.share, "docs", "2026"), filepath.Join(w.share, "music"), filepath.Join(w.share, ".shuttle-versions"))
	doc := fmt.Sprintf(`[shares.nas]
type = "path"
path = %q

[profiles.live]
mode = "sync"
triggers = ["realtime"]

[shuttles.docs]
profile = "live"
local = "~/Documents"
remote = "nas:/docs"
`, w.share)
	if err := os.WriteFile(o.Config, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	o.Browse = func(_ context.Context, sh *config.Share, _ secrets.Store) (fsys.FS, error) {
		if browseErr != nil {
			return nil, browseErr
		}
		return fsys.OpenLocal(sh.Path)
	}
	w.m = newModel(context.Background(), newFake(), o)
	w.m.every, w.m.now = 0, func() time.Time { return now }
	send(t, w.m, tea.WindowSizeMsg{Width: 150, Height: 30})
	run(t, w.m, w.m.Init())
	return w
}

func key(t *testing.T, m *model, code rune) {
	t.Helper()
	send(t, m, tea.KeyPressMsg{Code: code})
}

func TestNewShuttle(t *testing.T) {
	w := newWizardSetup(t, nil)
	m := w.m
	press(t, m, "n")
	if s := screen(m); !strings.Contains(s, "New shuttle") || !strings.Contains(s, "▸ nas") {
		t.Fatalf("first step:\n%s", s)
	}
	typeIn(t, m, "papers")
	key(t, m, tea.KeyEnter)

	// On the share: its folders, not the versions folder.
	s := screen(m)
	if !strings.Contains(s, "Folder on nas: /") || !strings.Contains(s, "docs/") || !strings.Contains(s, "music/") ||
		strings.Contains(s, ".shuttle-versions") || !strings.Contains(s, "Sync:  everything ") {
		t.Fatalf("on the share:\n%s", s)
	}
	key(t, m, tea.KeyRight) // docs opens in place
	if s := screen(m); !strings.Contains(s, "Folder on nas: /\n") || !strings.Contains(s, "› ▾ docs/\n    ▸ 2026/") {
		t.Fatalf("docs open:\n%s", s)
	}
	key(t, m, tea.KeyLeft)     // and closes
	press(t, m, "G", "g", "G") // music, the last
	press(t, m, "o")           // the shuttle's folder
	if s := screen(m); !strings.Contains(s, "Folder on nas: /music") || !strings.Contains(s, "Nothing inside") {
		t.Fatalf("in music:\n%s", s)
	}
	key(t, m, tea.KeyBackspace) // up: music stays open and selected
	if s := screen(m); !strings.Contains(s, "Folder on nas: /\n") || !strings.Contains(s, "› ▾ music/\n      empty") {
		t.Fatalf("not back on music:\n%s", s)
	}
	press(t, m, "o")        // music again
	key(t, m, tea.KeyEnter) // music it is

	// Here: the home's folders, the hidden ones after the rest, as a tree
	// like the share's.
	s = screen(m)
	if !strings.Contains(s, "Folder here: "+w.home) || !strings.Contains(s, "› ▸ Documents/\n  ▸ Papers/\n  ▸ .hidden/") ||
		!strings.Contains(s, "Sync:  everything ") {
		t.Fatalf("here:\n%s", s)
	}
	press(t, m, "down", "o") // Papers
	if s := screen(m); !strings.Contains(s, "Folder here: "+filepath.Join(w.home, "Papers")) {
		t.Fatalf("not in Papers:\n%s", s)
	}
	key(t, m, tea.KeyEnter)

	// The profile.
	if s := screen(m); !strings.Contains(s, "▸ none") || !strings.Contains(s, "live          sync, realtime") {
		t.Fatalf("profiles:\n%s", s)
	}
	press(t, m, "down")
	key(t, m, tea.KeyEnter)

	// Its own settings, over the profile's.
	if s := screen(m); !strings.Contains(s, "from live: sync") || !strings.Contains(s, "from live: realtime") {
		t.Fatalf("settings:\n%s", s)
	}
	toRow(t, m, "conflicts")
	key(t, m, tea.KeyLeft) // ask
	press(t, m, "tab")
	if s := screen(m); !strings.Contains(s, " ●Conflicts      ask") {
		t.Errorf("the override is not marked:\n%s", s)
	}
	ctrl(t, m, 's')
	if m.wizard != nil {
		t.Fatalf("still open: %s", m.wizard.result)
	}
	want := "[shuttles.papers]\nprofile = \"live\"\nlocal = \"~/Papers\"\nremote = \"nas:/music\"\nconflicts = \"ask\"\n"
	if got := configFile(t, m); !strings.HasSuffix(got, "\n\n"+want) {
		t.Errorf("config:\n%s\nwant it to end with:\n%s", got, want)
	}
	if m.selected != "papers" || !strings.Contains(screen(m), "saved the shuttle papers") {
		t.Errorf("selected %q:\n%s", m.selected, screen(m))
	}
}

func TestEditShuttle(t *testing.T) {
	w := newWizardSetup(t, nil)
	m := w.m
	before := configFile(t, m)
	press(t, m, "e") // docs, the first
	if s := screen(m); !strings.Contains(s, "The shuttle docs") || !strings.Contains(s, "renaming is not supported") {
		t.Fatalf("first step:\n%s", s)
	}
	key(t, m, tea.KeyEnter)
	if s := screen(m); !strings.Contains(s, "Folder on nas: /docs") {
		t.Fatalf("not at its folder:\n%s", s)
	}
	key(t, m, tea.KeyEnter)
	if s := screen(m); !strings.Contains(s, "Folder here: "+filepath.Join(w.home, "Documents")) {
		t.Fatalf("not at its folder here:\n%s", s)
	}
	key(t, m, tea.KeyEnter)
	if s := screen(m); !strings.Contains(s, "▸ live") {
		t.Fatalf("not on its profile:\n%s", s)
	}
	key(t, m, tea.KeyEnter)
	toRow(t, m, "mode")
	key(t, m, tea.KeyLeft) // sync, from not set
	key(t, m, tea.KeyLeft) // pull
	ctrl(t, m, 's')
	if m.question != "" {
		t.Errorf("asked %q for the same folders", m.question)
	}
	if got := configFile(t, m); got != strings.Replace(before, "remote = \"nas:/docs\"\n", "remote = \"nas:/docs\"\nmode = \"pull\"\n", 1) {
		t.Errorf("config:\n%s", got)
	}

	// Other folders: it asks first, and no leaves it.
	before = configFile(t, m)
	press(t, m, "e")
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyLeft)      // up to the top, docs open in it
	press(t, m, "down", "down") // past 2026, to music
	press(t, m, "o")
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	ctrl(t, m, 's')
	if !strings.Contains(screen(m), "docs now syncs other folders: it starts afresh") {
		t.Fatalf("no question:\n%s", screen(m))
	}
	press(t, m, "n")
	if configFile(t, m) != before {
		t.Error("saved after a no")
	}
	ctrl(t, m, 's')
	press(t, m, "y")
	if got := configFile(t, m); !strings.Contains(got, `remote = "nas:/music"`) {
		t.Errorf("config:\n%s", got)
	}
}

func TestShuttleFolders(t *testing.T) {
	w := newWizardSetup(t, errors.New("share nas: connecting: no route to host"))
	m := w.m
	press(t, m, "n")
	typeIn(t, m, "x")
	key(t, m, tea.KeyEnter)
	// The share cannot be reached: the path is typed instead.
	if s := screen(m); !strings.Contains(s, "no route to host") || !strings.Contains(s, "t types the path instead") {
		t.Fatalf("unreachable share:\n%s", s)
	}
	press(t, m, "t")
	for range 5 {
		key(t, m, tea.KeyBackspace)
	}
	typeIn(t, m, "/backups/x")
	key(t, m, tea.KeyEnter)

	// A folder here that is not there yet: created after asking.
	press(t, m, "t")
	newDir := filepath.Join(w.home, "Backups", "x")
	for range 80 {
		key(t, m, tea.KeyBackspace)
	}
	typeIn(t, m, "~/Backups/x")
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter) // no profile
	toRow(t, m, "mode")
	key(t, m, tea.KeyRight) // push
	ctrl(t, m, 's')
	if !strings.Contains(screen(m), "There is no folder "+newDir+" here. Create it?") {
		t.Fatalf("no question:\n%s", screen(m))
	}
	press(t, m, "y")
	if info, err := os.Stat(newDir); err != nil || !info.IsDir() {
		t.Errorf("not created: %v", err)
	}
	if got := configFile(t, m); !strings.HasSuffix(got, "[shuttles.x]\nlocal = \"~/Backups/x\"\nremote = \"nas:/backups/x\"\nmode = \"push\"\n") {
		t.Errorf("config:\n%s", got)
	}
}

func TestWizardRefuses(t *testing.T) {
	w := newWizardSetup(t, nil)
	m := w.m
	before := configFile(t, m)
	press(t, m, "n")
	key(t, m, tea.KeyEnter)
	if !strings.Contains(screen(m), "the shuttle needs a name") {
		t.Errorf("no name:\n%s", screen(m))
	}
	typeIn(t, m, "docs")
	key(t, m, tea.KeyEnter)
	if !strings.Contains(screen(m), "there is a shuttle named docs already") {
		t.Errorf("a taken name:\n%s", screen(m))
	}
	for range 4 {
		key(t, m, tea.KeyBackspace)
	}
	typeIn(t, m, "y")
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter) // no profile, and no mode of its own
	ctrl(t, m, 's')
	if s := screen(m); !strings.Contains(s, "shuttles.y: mode is not set") || m.wizard == nil {
		t.Errorf("no mode:\n%s", s)
	}
	if configFile(t, m) != before {
		t.Error("the config was written")
	}
	// esc goes back a step at a time, and out.
	for range 5 {
		press(t, m, "esc")
	}
	if m.wizard != nil {
		t.Errorf("still open at step %d", m.wizard.step)
	}
}

func TestDeleteShuttle(t *testing.T) {
	w := newWizardSetup(t, nil)
	m := w.m
	press(t, m, "x")
	if !strings.Contains(screen(m), "Delete the shuttle docs from the config file? Its files stay") {
		t.Fatalf("no question:\n%s", screen(m))
	}
	press(t, m, "y")
	if got := configFile(t, m); strings.Contains(got, "shuttles.docs") {
		t.Errorf("config:\n%s", got)
	}
}

func TestNoShareYet(t *testing.T) {
	m := start(t, newFake(), 120, 12)
	press(t, m, "n")
	if m.wizard != nil || !strings.Contains(screen(m), "there is no share yet: add one on the Shares tab (5)") {
		t.Errorf("no share:\n%s", screen(m))
	}
}

// Off its profile, with triggers of none: the profile line goes, and
// triggers = [] overrides nothing with nothing.
func TestShuttleWithoutItsProfile(t *testing.T) {
	w := newWizardSetup(t, nil)
	m := w.m
	press(t, m, "e")
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	press(t, m, "up") // none
	key(t, m, tea.KeyEnter)
	toRow(t, m, "mode")
	key(t, m, tea.KeyRight) // push
	toRow(t, m, "triggers")
	typeIn(t, m, "none")
	ctrl(t, m, 's')
	got := configFile(t, m)
	if strings.Contains(got, "profile = ") || !strings.Contains(got, "mode = \"push\"\ntriggers = []\n") {
		t.Errorf("config:\n%s", got)
	}
}

// Marking what to sync: only what is marked, then all but what is marked,
// then everything again, each saved as the shuttle's include or exclude.
func TestMarkWhatToSync(t *testing.T) {
	w := newWizardSetup(t, nil)
	m := w.m
	for _, f := range []string{"docs/2026/a.txt", "docs/2026/b.txt", "docs/notes.txt", "docs/old/x.txt"} {
		mkdirs(t, filepath.Dir(filepath.Join(w.share, f)))
		if err := os.WriteFile(filepath.Join(w.share, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	press(t, m, "e") // docs
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyBackspace) // up a folder: docs open in it, folders first
	if s := screen(m); !strings.Contains(s, "Folder on nas: /\n") ||
		!strings.Contains(s, "› ▾ docs/\n    ▸ 2026/\n    ▸ old/\n      notes.txt\n  ▸ music/") {
		t.Fatalf("docs open:\n%s", s)
	}
	key(t, m, tea.KeySpace)
	if s := screen(m); !strings.Contains(s, "everything is synced: m chooses") {
		t.Errorf("space with everything synced:\n%s", s)
	}

	press(t, m, "m") // only what is marked
	if s := screen(m); !strings.Contains(s, " only what is marked ") || !strings.Contains(s, "Nothing marked yet") ||
		!strings.Contains(s, "[ ] docs/") {
		t.Fatalf("only what is marked:\n%s", s)
	}
	key(t, m, tea.KeyEnter)
	if s := screen(m); !strings.Contains(s, "nothing is marked to sync") {
		t.Fatalf("went on with nothing marked:\n%s", s)
	}
	press(t, m, "down") // 2026
	key(t, m, tea.KeySpace)
	key(t, m, tea.KeyRight)
	if s := screen(m); !strings.Contains(s, "[~] docs/") || !strings.Contains(s, "[✓] 2026/") ||
		!strings.Contains(s, "[✓] a.txt") || !strings.Contains(s, "[ ] old/") || !strings.Contains(s, "[ ] music/") {
		t.Fatalf("2026 marked:\n%s", s)
	}
	press(t, m, "down") // a.txt: unmarking it marks b.txt instead of 2026
	key(t, m, tea.KeySpace)
	if s := screen(m); !strings.Contains(s, "[~] 2026/") || !strings.Contains(s, "[ ] a.txt") || !strings.Contains(s, "[✓] b.txt") {
		t.Fatalf("a.txt unmarked:\n%s", s)
	}
	press(t, m, "down", "down", "down") // notes.txt
	key(t, m, tea.KeySpace)
	if s := screen(m); !strings.Contains(s, "Synced only: docs/2026/b.txt, docs/notes.txt") {
		t.Fatalf("marks:\n%s", s)
	}
	key(t, m, tea.KeyLeft) // to docs
	press(t, m, "o")       // its folder again: the marks are inside it
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	ctrl(t, m, 's')
	if got := configFile(t, m); !strings.Contains(got, "remote = \"nas:/docs\"\ninclude = [\"2026/b.txt\", \"notes.txt\"]\n") {
		t.Fatalf("config:\n%s", got)
	}

	// Edited: the marks are back; all but them is synced now.
	press(t, m, "e")
	key(t, m, tea.KeyEnter)
	if s := screen(m); !strings.Contains(s, "Folder on nas: /docs") || !strings.Contains(s, " only what is marked ") ||
		!strings.Contains(s, "Synced only: 2026/b.txt, notes.txt") {
		t.Fatalf("marks not back:\n%s", s)
	}
	press(t, m, "m")
	if s := screen(m); !strings.Contains(s, "Left out: 2026/b.txt, notes.txt") || !strings.Contains(s, "[✓] old/") ||
		!strings.Contains(s, "[~] 2026/") || !strings.Contains(s, "[ ] notes.txt") {
		t.Fatalf("all but what is marked:\n%s", s)
	}
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	ctrl(t, m, 's')
	got := configFile(t, m)
	if !strings.Contains(got, "exclude = [\"2026/b.txt\", \"notes.txt\"]\n") || strings.Contains(got, "include") {
		t.Fatalf("config:\n%s", got)
	}

	// And everything again: neither is left.
	press(t, m, "e")
	key(t, m, tea.KeyEnter)
	press(t, m, "m")
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	ctrl(t, m, 's')
	if got := configFile(t, m); strings.Contains(got, "include") || strings.Contains(got, "exclude") {
		t.Errorf("config:\n%s", got)
	}
}

// A listing that comes back for a folder no longer shown is dropped.
func TestTreeDropsStaleListings(t *testing.T) {
	tr := newTree("on nas", "/docs", nil)
	tr.listed(treeListedMsg{id: tr.id, dir: "/elsewhere", entries: []entry{{name: "stale"}}})
	tr.listed(treeListedMsg{id: tr.id + 1, dir: "/docs", entries: []entry{{name: "other tree"}}})
	if len(tr.root.kids) != 0 {
		t.Errorf("kids = %v", tr.root.kids)
	}
	tr.listed(treeListedMsg{id: tr.id, dir: "/docs", entries: []entry{{name: "fresh"}}})
	if len(tr.root.kids) != 1 || tr.root.kids[0].path != "/docs/fresh" {
		t.Errorf("kids = %v", tr.root.kids)
	}
}

// The shuttle has one list of marks for both sides: what is marked on the
// share shows here, what is marked here shows on the share, and both are
// saved, even what is only on one side.
func TestMarksOnBothSides(t *testing.T) {
	w := newWizardSetup(t, nil)
	m := w.m
	mkdirs(t, filepath.Join(w.share, "docs", "raw"), filepath.Join(w.home, "Documents", "raw"),
		filepath.Join(w.home, "Documents", "scratch"))
	press(t, m, "e") // docs
	key(t, m, tea.KeyEnter)
	press(t, m, "m", "m") // all but what is marked
	press(t, m, "down")   // raw, after 2026
	key(t, m, tea.KeySpace)
	key(t, m, tea.KeyEnter)
	if s := screen(m); !strings.Contains(s, "Folder here: "+filepath.Join(w.home, "Documents")) ||
		!strings.Contains(s, " all but what is marked ") || !strings.Contains(s, "Left out: raw/") ||
		!strings.Contains(s, "[ ] raw/") || !strings.Contains(s, "[✓] scratch/") {
		t.Fatalf("the share's marks, here:\n%s", s)
	}
	press(t, m, "down") // scratch, only here
	key(t, m, tea.KeySpace)
	if s := screen(m); !strings.Contains(s, "Left out: raw/, scratch/") {
		t.Fatalf("marked here:\n%s", s)
	}
	press(t, m, "esc")
	if s := screen(m); !strings.Contains(s, "Folder on nas: /docs") || !strings.Contains(s, "Left out: raw/, scratch/") {
		t.Fatalf("the marks made here, on the share:\n%s", s)
	}
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	key(t, m, tea.KeyEnter)
	ctrl(t, m, 's')
	if got := configFile(t, m); !strings.Contains(got, "exclude = [\"raw\", \"scratch\"]\n") {
		t.Fatalf("config:\n%s", got)
	}

	// Only what is marked, with the marks undone here: it will not go on.
	press(t, m, "e")
	key(t, m, tea.KeyEnter)
	press(t, m, "m", "m") // only what is marked, the same marks
	key(t, m, tea.KeyEnter)
	if s := screen(m); !strings.Contains(s, "› ▸ [✓] raw/\n  ▸ [✓] scratch/") {
		t.Fatalf("only what is marked, here:\n%s", s)
	}
	key(t, m, tea.KeySpace)
	press(t, m, "down")
	key(t, m, tea.KeySpace)
	key(t, m, tea.KeyEnter)
	if s := screen(m); !strings.Contains(s, "nothing is marked to sync") {
		t.Fatalf("went on with nothing marked:\n%s", s)
	}
}

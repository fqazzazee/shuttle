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
		strings.Contains(s, ".shuttle-versions") {
		t.Fatalf("on the share:\n%s", s)
	}
	key(t, m, tea.KeyRight) // into docs
	if s := screen(m); !strings.Contains(s, "Folder on nas: /docs") || !strings.Contains(s, "2026/") {
		t.Fatalf("in docs:\n%s", s)
	}
	key(t, m, tea.KeyLeft)     // up to the top, docs selected
	press(t, m, "G", "g", "G") // music, the last
	key(t, m, tea.KeyRight)
	key(t, m, tea.KeyLeft) // and out again: music stays selected
	if s := screen(m); !strings.Contains(s, "▸ music/") {
		t.Fatalf("not back on music:\n%s", s)
	}
	key(t, m, tea.KeyRight) // into music
	key(t, m, tea.KeyEnter) // music it is

	// Here: the home's folders, the hidden ones left out.
	s = screen(m)
	if !strings.Contains(s, "Folder here: "+w.home) || !strings.Contains(s, "Documents/") || strings.Contains(s, ".hidden") {
		t.Fatalf("here:\n%s", s)
	}
	press(t, m, "down") // Papers
	key(t, m, tea.KeyRight)
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
	key(t, m, tea.KeyLeft) // up to the top
	press(t, m, "down")    // music
	key(t, m, tea.KeyRight)
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

// A listing that comes back for a folder no longer shown is dropped.
func TestBrowserDropsStaleListings(t *testing.T) {
	b := localBrowser(t.TempDir())
	b.listed(listedMsg{id: b.id, dir: "/elsewhere", entries: []string{"stale"}})
	b.listed(listedMsg{id: b.id + 1, dir: b.dir, entries: []string{"other browser"}})
	if len(b.entries) != 0 {
		t.Errorf("entries = %q", b.entries)
	}
	b.listed(listedMsg{id: b.id, dir: b.dir, entries: []string{"fresh"}})
	if len(b.entries) != 1 {
		t.Errorf("entries = %q", b.entries)
	}
}

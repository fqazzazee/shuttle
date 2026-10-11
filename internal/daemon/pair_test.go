// SPDX-License-Identifier: GPL-2.0-or-later

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/fqazzazee/shuttle/internal/config"
)

// Pointing a shuttle at other folders must not read the old folders'
// history into them: the files the new local folder lacks were never
// deleted there, so they must not be deleted from the share.
func TestNewFoldersStartAfresh(t *testing.T) {
	f := newFixture(t, "sync", "")
	for i := range 5 {
		write(t, f.local, fmt.Sprintf("f%d", i), "x")
	}
	f.start()
	f.syncNow()
	f.stop()

	// The user edits the config: same shuttle, a new, empty local folder.
	other := t.TempDir()
	g := &fixture{t: t, paths: f.paths, local: other, mount: f.mount}
	g.writeConfig(fmt.Sprintf("[shares.mnt]\ntype = \"path\"\npath = %q\n[shuttles.s]\nmode = \"sync\"\nlocal = %q\nremote = \"mnt:\"\n", f.mount, other))
	g.start()
	g.syncNow()
	for i := range 5 {
		name := fmt.Sprintf("f%d", i)
		if !exists(f.mount, name) {
			t.Errorf("%s was deleted from the share", name)
		}
		if !exists(other, name) {
			t.Errorf("%s was not copied into the new local folder", name)
		}
	}
}

// Restarting with the same folders keeps the history: a file deleted
// locally while the daemon was down is deleted from the share too.
func TestSameFoldersKeepTheirHistory(t *testing.T) {
	f := newFixture(t, "sync", "")
	write(t, f.local, "a", "x")
	write(t, f.local, "b", "x")
	f.start()
	f.syncNow()
	f.stop()
	os.Remove(filepath.Join(f.local, "a"))

	g := &fixture{t: t, paths: f.paths, local: f.local, mount: f.mount}
	g.start()
	g.syncNow()
	if exists(f.mount, "a") || !exists(f.mount, "b") {
		t.Error("the deletion was not synced: the history was lost on restart")
	}
}

func TestPairOf(t *testing.T) {
	base := func() (*config.Config, *config.Shuttle) {
		return &config.Config{Shares: map[string]config.Share{
				"nas": {Name: "nas", Type: config.ShareSMB, Host: "NAS.example", Share: "Home", User: "user"},
			}}, &config.Shuttle{Name: "docs", Local: "/home/u/Documents", Share: "nas", Remote: "docs",
				Settings: config.Settings{Mode: config.ModeSync}}
	}
	cfg, s := base()
	want := pairOf(cfg, s)
	for _, tc := range []struct {
		name   string
		change func(*config.Config, *config.Shuttle)
		same   bool
	}{
		{"another user", func(c *config.Config, _ *config.Shuttle) {
			sh := c.Shares["nas"]
			sh.User = "admin"
			c.Shares["nas"] = sh
		}, true},
		{"the host's case", func(c *config.Config, _ *config.Shuttle) {
			sh := c.Shares["nas"]
			sh.Host = "nas.EXAMPLE"
			c.Shares["nas"] = sh
		}, true},
		{"the default port spelled out", func(c *config.Config, _ *config.Shuttle) { sh := c.Shares["nas"]; sh.Port = 445; c.Shares["nas"] = sh }, true},
		{"the share renamed in the config", func(c *config.Config, s *config.Shuttle) {
			sh := c.Shares["nas"]
			sh.Name = "home"
			c.Shares["home"] = sh
			s.Share = "home"
		}, true},
		{"another mode", func(_ *config.Config, s *config.Shuttle) { s.Settings.Mode = config.ModePush }, true},
		{"another local folder", func(_ *config.Config, s *config.Shuttle) { s.Local = "/home/u/Docs" }, false},
		{"another remote folder", func(_ *config.Config, s *config.Shuttle) { s.Remote = "documents" }, false},
		{"another server", func(c *config.Config, _ *config.Shuttle) {
			sh := c.Shares["nas"]
			sh.Host = "nas2.example"
			c.Shares["nas"] = sh
		}, false},
		{"another share on the server", func(c *config.Config, _ *config.Shuttle) {
			sh := c.Shares["nas"]
			sh.Share = "backup"
			c.Shares["nas"] = sh
		}, false},
		{"another port", func(c *config.Config, _ *config.Shuttle) { sh := c.Shares["nas"]; sh.Port = 1445; c.Shares["nas"] = sh }, false},
	} {
		cfg, s := base()
		tc.change(cfg, s)
		if got := pairOf(cfg, s) == want; got != tc.same {
			t.Errorf("%s: same pair = %v, want %v", tc.name, got, tc.same)
		}
	}
}

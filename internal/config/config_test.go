// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

const home = "/home/user"

const day = 24 * time.Hour

func parse(t *testing.T, doc string) *Config {
	t.Helper()
	cfg, err := Parse([]byte(doc), home)
	if err != nil {
		t.Fatalf("Parse failed:\n%v", err)
	}
	return cfg
}

func mustShuttle(t *testing.T, cfg *Config, name string) Shuttle {
	t.Helper()
	s, ok := cfg.Shuttle(name)
	if !ok {
		t.Fatalf("no shuttle %q", name)
	}
	return s
}

func TestExampleConfig(t *testing.T) {
	data, err := os.ReadFile("testdata/example.toml")
	if err != nil {
		t.Fatal(err)
	}
	cfg := parse(t, string(data))

	if cfg.Bandwidth != (Bandwidth{Up: 50e6}) {
		t.Errorf("global bandwidth = %+v", cfg.Bandwidth)
	}
	if got := cfg.Shares["mounted"]; got.Type != SharePath || got.Path != "/mnt/nas" {
		t.Errorf("mounted share = %+v", got)
	}
	if !slices.Equal(cfg.Profiles, []string{"backup", "live", "manual"}) {
		t.Errorf("profiles = %v", cfg.Profiles)
	}
	var names []string
	for _, s := range cfg.Shuttles {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"archive", "documents", "photos", "projects"}) {
		t.Errorf("shuttles = %v, want sorted names", names)
	}

	t.Run("documents inherits its profile", func(t *testing.T) {
		s := mustShuttle(t, cfg, "documents")
		if s.Local != "/home/user/Documents" || s.Share != "nas" || s.Remote != "docs" || !s.Enabled {
			t.Errorf("folders = %q %q:%q enabled=%v", s.Local, s.Share, s.Remote, s.Enabled)
		}
		st := s.Settings
		if st.Mode != ModeSync || !slices.Equal(st.Triggers, []Trigger{TriggerRealtime, TriggerShareAvailable}) {
			t.Errorf("mode/triggers = %v %v", st.Mode, st.Triggers)
		}
		want := Versioning{Kind: VersioningStaggered, Keep: 365 * day, SizeCap: 20e9}
		if st.Versioning != want {
			t.Errorf("versioning = %+v, want %+v", st.Versioning, want)
		}
		if st.Notify.Completed {
			t.Error("completion notices should default off for a real-time shuttle")
		}
		for key, src := range map[string]Source{
			"mode": SourceProfile, "versioning": SourceProfile, "deletions": SourceDefault,
			"notify.completed": SourceDefault,
		} {
			if s.Sources[key] != src {
				t.Errorf("Sources[%q] = %v, want %v", key, s.Sources[key], src)
			}
		}
	})

	t.Run("projects adds ignores and overrides one notify switch", func(t *testing.T) {
		s := mustShuttle(t, cfg, "projects")
		want := []string{"*.tmp", "~$*", ".DS_Store", "node_modules/", "bin/"}
		if !slices.Equal(s.Settings.Ignore, want) {
			t.Errorf("ignore = %v, want %v", s.Settings.Ignore, want)
		}
		if !s.Settings.Notify.Completed || s.Sources["notify.completed"] != SourceShuttle {
			t.Errorf("notify.completed = %v from %v, want true from shuttle", s.Settings.Notify.Completed, s.Sources["notify.completed"])
		}
		if !s.Settings.Notify.Errors || s.Sources["notify.errors"] != SourceDefault {
			t.Error("the other notify switches should keep their defaults")
		}
		// The profile's list must not have been modified in place.
		doc := mustShuttle(t, cfg, "documents")
		if len(doc.Settings.Ignore) != 3 {
			t.Errorf("documents ignore = %v: ignore_add leaked into another shuttle", doc.Settings.Ignore)
		}
	})

	t.Run("photos merges bandwidth per direction", func(t *testing.T) {
		s := mustShuttle(t, cfg, "photos")
		if s.Settings.Bandwidth != (Bandwidth{Up: 20e6, Down: 5e6}) {
			t.Errorf("bandwidth = %+v", s.Settings.Bandwidth)
		}
		if s.Sources["bandwidth.up"] != SourceProfile || s.Sources["bandwidth.down"] != SourceShuttle {
			t.Errorf("bandwidth sources = %v / %v", s.Sources["bandwidth.up"], s.Sources["bandwidth.down"])
		}
		if !slices.Equal(s.Settings.At, []TimeOfDay{{2, 0}}) {
			t.Errorf("at = %v", s.Settings.At)
		}
		if !s.Settings.Notify.Completed {
			t.Error("completion notices should default on for a scheduled shuttle")
		}
	})

	t.Run("archive replaces versioning as a unit", func(t *testing.T) {
		s := mustShuttle(t, cfg, "archive")
		if s.Enabled {
			t.Error("archive should be disabled")
		}
		if s.Settings.Versioning != (Versioning{Kind: VersioningSimple, Count: 3}) {
			t.Errorf("versioning = %+v", s.Settings.Versioning)
		}
		if len(s.Settings.Triggers) != 0 {
			t.Errorf("triggers = %v, want manual only", s.Settings.Triggers)
		}
		if s.Share != "mounted" || s.Remote != "old" {
			t.Errorf("remote = %s:%s", s.Share, s.Remote)
		}
	})
}

const minimalShare = `
[shares.nas]
type = "smb"
host = "nas.example"
share = "home"
`

func TestDefaults(t *testing.T) {
	cfg := parse(t, minimalShare+`
[shuttles.s]
mode = "push"
local = "/data"
remote = "nas:"
`)
	s := mustShuttle(t, cfg, "s")
	want := Defaults()
	want.Mode = ModePush
	if !settingsEqual(&s.Settings, &want) {
		t.Errorf("settings = %+v\nwant       %+v", s.Settings, want)
	}
	if s.Settings.Deletions != DeletionsAdditive || s.Settings.Versioning.Kind != VersioningTrash {
		t.Error("defaults must never delete: additive push/pull and versioning on")
	}
	if s.Remote != "." {
		t.Errorf("remote %q, want the share root \".\"", s.Remote)
	}
}

func settingsEqual(a, b *Settings) bool {
	return a.Mode == b.Mode && slices.Equal(a.Triggers, b.Triggers) && a.PollInterval == b.PollInterval &&
		a.Interval == b.Interval && slices.Equal(a.At, b.At) && a.Conflicts == b.Conflicts &&
		a.ConflictHold == b.ConflictHold && a.Deletions == b.Deletions && a.DeletionGuard == b.DeletionGuard &&
		a.Versioning == b.Versioning && slices.Equal(a.Ignore, b.Ignore) && a.Bandwidth == b.Bandwidth &&
		a.Notify == b.Notify
}

func TestShuttleOverridesProfile(t *testing.T) {
	cfg := parse(t, minimalShare+`
[profiles.p]
mode = "sync"
conflicts = "ask"
deletion_guard = 20
ignore = ["a", "b"]

[shuttles.s]
profile = "p"
local = "/data"
remote = "nas:/x"
conflicts = "newest-wins"
ignore = ["c"]
deletion_guard = 0
`)
	s := mustShuttle(t, cfg, "s")
	if s.Settings.Conflicts != ConflictNewestWins || s.Sources["conflicts"] != SourceShuttle {
		t.Errorf("conflicts = %v from %v", s.Settings.Conflicts, s.Sources["conflicts"])
	}
	if !slices.Equal(s.Settings.Ignore, []string{"c"}) {
		t.Errorf("ignore = %v: a shuttle's ignore replaces the profile's", s.Settings.Ignore)
	}
	if s.Settings.DeletionGuard != 0 || s.Sources["deletion_guard"] != SourceShuttle {
		t.Error("an explicit 0 must override the profile, not read as unset")
	}
	if s.Settings.Mode != ModeSync || s.Sources["mode"] != SourceProfile {
		t.Errorf("mode = %v from %v", s.Settings.Mode, s.Sources["mode"])
	}
}

func TestHomeExpansion(t *testing.T) {
	cfg := parse(t, minimalShare+`
[shuttles.a]
mode = "pull"
local = "~/docs/"
remote = "nas:/a"

[shares.m]
type = "path"
path = "~/mnt/"

[shuttles.b]
mode = "pull"
local = "/srv/b/"
remote = "m:/b/"
`)
	if a := mustShuttle(t, cfg, "a"); a.Local != home+"/docs" {
		t.Errorf("local = %q, want %q", a.Local, home+"/docs")
	}
	if got := (&parser{home: home}).expandHome("~"); got != home {
		t.Errorf(`expandHome("~") = %q, want %q`, got, home)
	}
	if got := cfg.Shares["m"].Path; got != home+"/mnt" {
		t.Errorf("share path = %q", got)
	}
	if b := mustShuttle(t, cfg, "b"); b.Local != "/srv/b" || b.Remote != "b" {
		t.Errorf("b = %q %q, want trailing slashes trimmed", b.Local, b.Remote)
	}
}

func TestInvalidConfigs(t *testing.T) {
	tests := []struct {
		name, doc, want string
	}{
		{"password in file", `
[shares.nas]
type = "smb"
host = "h"
share = "s"
password = "hunter2"`, "keyring"},
		{"unknown key", minimalShare + `
[shuttles.s]
mode = "push"
local = "/d"
remote = "nas:"
deletes = "mirror"`, `line 11: unknown key "shuttles.s.deletes"`},
		{"no mode", minimalShare + `
[shuttles.s]
local = "/d"
remote = "nas:"`, "mode is not set"},
		{"bad mode", minimalShare + `
[shuttles.s]
mode = "both"
local = "/d"
remote = "nas:"`, `mode: "both" must be one of push, pull, sync`},
		{"bad trigger", minimalShare + `
[shuttles.s]
mode = "sync"
triggers = ["hourly"]
local = "/d"
remote = "nas:"`, `"hourly" must be one of`},
		{"schedule without time", minimalShare + `
[shuttles.s]
mode = "push"
triggers = ["schedule"]
local = "/d"
remote = "nas:"`, `"schedule" needs interval or at`},
		{"schedule too often", minimalShare + `
[shuttles.s]
mode = "push"
triggers = ["schedule"]
interval = "10s"
local = "/d"
remote = "nas:"`, "interval: must be at least 1m"},
		{"bad time", minimalShare + `
[shuttles.s]
mode = "push"
at = ["25:00"]
local = "/d"
remote = "nas:"`, "invalid time"},
		{"guard over 100", minimalShare + `
[shuttles.s]
mode = "push"
deletion_guard = 150
local = "/d"
remote = "nas:"`, "deletion_guard: 150"},
		{"key not for kind", minimalShare + `
[shuttles.s]
mode = "push"
versioning = { kind = "trash", count = 3 }
local = "/d"
remote = "nas:"`, `count: does not apply to kind "trash"`},
		{"bad versioning kind", minimalShare + `
[shuttles.s]
mode = "push"
versioning = { kind = "forever" }
local = "/d"
remote = "nas:"`, `kind: "forever" must be one of`},
		{"bad size", minimalShare + `
[shuttles.s]
mode = "push"
versioning = { kind = "trash", size_cap = "lots" }
local = "/d"
remote = "nas:"`, "invalid size"},
		{"relative local", minimalShare + `
[shuttles.s]
mode = "push"
local = "data"
remote = "nas:"`, "must be an absolute path"},
		{"remote escapes share", minimalShare + `
[shuttles.s]
mode = "push"
local = "/d"
remote = "nas:/a/../../etc"`, "not a clean path"},
		{"remote double slash", minimalShare + `
[shuttles.s]
mode = "push"
local = "/d"
remote = "nas:/a//b"`, "not a clean path"},
		{"remote without share", minimalShare + `
[shuttles.s]
mode = "push"
local = "/d"
remote = "/docs"`, "must be share:path"},
		{"unknown share", minimalShare + `
[shuttles.s]
mode = "push"
local = "/d"
remote = "nope:/docs"`, `no share named "nope"`},
		{"unknown profile", minimalShare + `
[shuttles.s]
profile = "nope"
mode = "push"
local = "/d"
remote = "nas:"`, `no profile named "nope"`},
		{"bad name", minimalShare + `
[shuttles."../evil"]
mode = "push"
local = "/d"
remote = "nas:"`, "name must start with"},
		{"smb without host", `
[shares.nas]
type = "smb"
share = "s"`, `host is required for type "smb"`},
		{"bad share type", `
[shares.nas]
type = "ftp"`, `type: "ftp" must be one of`},
		{"overlapping locals", minimalShare + `
[shuttles.a]
mode = "push"
local = "/data"
remote = "nas:/a"

[shuttles.b]
mode = "push"
local = "/data/sub"
remote = "nas:/b"`, "shuttles.a and shuttles.b: folders overlap"},
		{"overlapping remotes", minimalShare + `
[shuttles.a]
mode = "push"
local = "/a"
remote = "nas:/docs"

[shuttles.b]
mode = "push"
local = "/b"
remote = "nas:"`, "folders overlap"},
		{"mounted share overlaps its own local", `
[shares.m]
type = "path"
path = "/mnt/nas"

[shuttles.s]
mode = "sync"
local = "/mnt/nas/docs/me"
remote = "m:/docs"`, "local and remote folders overlap"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.doc), home)
			if err == nil {
				t.Fatalf("Parse succeeded, want an error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error:\n%v\nwant it to contain %q", err, tt.want)
			}
		})
	}
}

func TestSiblingFoldersDoNotOverlap(t *testing.T) {
	parse(t, minimalShare+`
[shuttles.a]
mode = "push"
local = "/data/photos"
remote = "nas:/photos"

[shuttles.b]
mode = "push"
local = "/data/photos-old"
remote = "nas:/photos-old"
`)
}

func TestAllErrorsReportedOnce(t *testing.T) {
	_, err := Parse([]byte(minimalShare+`
[profiles.p]
mode = "sideways"

[shuttles.a]
profile = "p"
local = "/a"
remote = "nas:/a"

[shuttles.b]
profile = "p"
local = "relative"
remote = "nas:/b"
`), home)
	if err == nil {
		t.Fatal("Parse succeeded")
	}
	msg := err.Error()
	if n := strings.Count(msg, `"sideways"`); n != 1 {
		t.Errorf("the profile error appears %d times, want once:\n%s", n, msg)
	}
	if !strings.Contains(msg, "shuttles.b.local") {
		t.Errorf("errors from other tables are missing:\n%s", msg)
	}
}

func TestLoad(t *testing.T) {
	if _, err := Load("testdata/example.toml"); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("testdata/missing.toml"); err == nil {
		t.Error("Load of a missing file succeeded")
	}
}

// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func enabled(t *testing.T, path, name string) bool {
	t.Helper()
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Shuttles {
		if cfg.Shuttles[i].Name == name {
			return cfg.Shuttles[i].Enabled
		}
	}
	t.Fatalf("no shuttle %s", name)
	return false
}

// Turning a shuttle off and on changes its enabled line and nothing else.
func TestSetEnabled(t *testing.T) {
	path := writeConfig(t, commented)
	home := t.TempDir()

	changed, err := SetEnabled(path, home, "docs", false)
	if err != nil || !changed {
		t.Fatalf("disable docs = %v, %v", changed, err)
	}
	want := strings.Replace(commented, "remote = \"nas:/docs\"\n", "remote = \"nas:/docs\"\nenabled = false\n", 1)
	if got := mustRead(t, path); got != want {
		t.Errorf("after disabling:\n%s\nwant:\n%s", got, want)
	}
	if changed, err := SetEnabled(path, home, "docs", false); err != nil || changed {
		t.Errorf("disabling it again = %v, %v; want no change", changed, err)
	}

	if changed, err := SetEnabled(path, home, "docs", true); err != nil || !changed {
		t.Fatalf("enable docs = %v, %v", changed, err)
	}
	if got := mustRead(t, path); got != commented {
		t.Errorf("enabling did not give back the file:\n%s", got)
	}
	if changed, err := SetEnabled(path, home, "photos", true); err != nil || changed {
		t.Errorf("enabling a shuttle that is on = %v, %v; want no change", changed, err)
	}
}

func TestSetEnabledSpellings(t *testing.T) {
	home := t.TempDir()
	const share = "[shares.m]\ntype = \"path\"\npath = \"/mnt\"\n"
	for _, tc := range []struct{ name, text, shuttle string }{
		{"explicitly on", "[shuttles.s]\nmode = \"push\"\nlocal = \"/l\"\nremote = \"m:\"\nenabled = true # on\n", "s"},
		{"a quoted name", "[shuttles.\"my.docs\"]\nmode = \"push\"\nlocal = \"/l\"\nremote = \"m:\"\n", "my.docs"},
		{"an inline table", "[shuttles]\ns = { mode = \"push\", local = \"/l\", remote = \"m:\" }\n", "s"},
		{"dotted keys", "[shuttles]\ns.mode = \"push\"\ns.local = \"/l\"\ns.remote = \"m:\"\n", "s"},
	} {
		path := writeConfig(t, share+tc.text)
		if _, err := SetEnabled(path, home, tc.shuttle, false); err != nil {
			t.Errorf("%s: disable: %v", tc.name, err)
			continue
		}
		if enabled(t, path, tc.shuttle) {
			t.Errorf("%s: still on after disabling:\n%s", tc.name, mustRead(t, path))
		}
		if _, err := SetEnabled(path, home, tc.shuttle, true); err != nil {
			t.Errorf("%s: enable: %v", tc.name, err)
			continue
		}
		if !enabled(t, path, tc.shuttle) {
			t.Errorf("%s: still off after enabling:\n%s", tc.name, mustRead(t, path))
		}
	}
}

func TestSetEnabledRefuses(t *testing.T) {
	home := t.TempDir()
	path := writeConfig(t, commented)
	if _, err := SetEnabled(path, home, "nope", false); !errors.Is(err, ErrNoShuttle) {
		t.Errorf("an unknown shuttle: %v, want ErrNoShuttle", err)
	}
	broken := strings.Replace(commented, `mode = "push"`, `mode = "both"`, 1)
	path = writeConfig(t, broken)
	if _, err := SetEnabled(path, home, "photos", false); err == nil || !strings.Contains(err.Error(), `"both"`) {
		t.Errorf("a broken config: %v, want its error", err)
	}
	if got := mustRead(t, path); got != broken {
		t.Error("a refused edit changed the file")
	}
	if _, err := SetEnabled(filepath.Join(home, "none.toml"), home, "s", false); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no config file: %v", err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

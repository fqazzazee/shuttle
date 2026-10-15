// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/config"
)

// A setting changed through the daemon is written to the config file, and
// the daemon runs with it at once.
func TestSetShuttleSetting(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", "")
	f.start()
	if err := f.d.SetShuttleSetting("s", "metered", "run"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(f.paths.Config); !strings.Contains(string(data), `metered = "run"`) {
		t.Errorf("config:\n%s", data)
	}
	f.waitFor("s", "the shuttle running with it", func(Status) bool {
		r := f.runnerOf("s")
		return r != nil && r.shuttle.Settings.Metered == config.MeteredRun
	})
	rows, err := f.d.ShuttleSettings("s")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == "metered" && r.Value != "run" {
			t.Errorf("metered row %+v, want the shuttle's own run", r)
		}
	}

	if err := f.d.SetShuttleSetting("s", "metered", "sometimes"); err == nil || errors.Is(err, ErrUnknownShuttle) {
		t.Errorf("a value none of the choices: %v", err)
	}
	if err := f.d.SetShuttleSetting("nope", "metered", "run"); !errors.Is(err, ErrUnknownShuttle) {
		t.Errorf("SetShuttleSetting(nope) = %v, want ErrUnknownShuttle", err)
	}
	if _, err := f.d.ShuttleSettings("nope"); !errors.Is(err, ErrUnknownShuttle) {
		t.Errorf("ShuttleSettings(nope) = %v, want ErrUnknownShuttle", err)
	}
}

// A profile's setting changed through the daemon is written to the config
// file, and its shuttles run with it at once.
func TestSetProfileSetting(t *testing.T) {
	quickReloads(t)
	f := newFixture(t, "push", "profile = \"p\"\n\n[profiles.p]\nmetered = \"pause\"")
	f.start()
	if err := f.d.SetProfileSetting("p", "metered", "run"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(f.paths.Config); !strings.Contains(string(data), "[profiles.p]\nmetered = \"run\"") {
		t.Errorf("config:\n%s", data)
	}
	f.waitFor("s", "the shuttle running with its profile's", func(Status) bool {
		r := f.runnerOf("s")
		return r != nil && r.shuttle.Settings.Metered == config.MeteredRun
	})
	rows, err := f.d.ProfileSettings("p")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == "metered" && r.Value != "run" {
			t.Errorf("metered row %+v, want the profile's own run", r)
		}
	}

	if err := f.d.SetProfileSetting("p", "metered", "sometimes"); err == nil || errors.Is(err, config.ErrNoProfile) {
		t.Errorf("a value none of the choices: %v", err)
	}
	if err := f.d.SetProfileSetting("nope", "metered", "run"); !errors.Is(err, config.ErrNoProfile) {
		t.Errorf("SetProfileSetting(nope) = %v, want ErrNoProfile", err)
	}
	if _, err := f.d.ProfileSettings("nope"); !errors.Is(err, config.ErrNoProfile) {
		t.Errorf("ProfileSettings(nope) = %v, want ErrNoProfile", err)
	}
}

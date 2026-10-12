// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestRawTable(t *testing.T) {
	doc := []byte(`[profiles.live]
mode = "sync"
triggers = ["realtime"]
deletion_guard = 20
versioning = { kind = "trash", keep = "30d" }

[profiles.live.notify]
completed = false
`)
	got, err := RawTable(doc, "profiles", "live")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"mode": "sync", "triggers": []any{"realtime"}, "deletion_guard": int64(20),
		"versioning": map[string]any{"kind": "trash", "keep": "30d"},
		"notify":     map[string]any{"completed": false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RawTable = %#v\nwant %#v", got, want)
	}
	if got, err := RawTable(doc, "profiles", "none"); got != nil || err != nil {
		t.Errorf("no such table: %v, %v", got, err)
	}
	if _, err := RawTable([]byte("x = "), "profiles"); err == nil {
		t.Error("a broken file read")
	}
}

func TestTerminal(t *testing.T) {
	cfg, err := Parse([]byte("[global]\nterminal = \"ptyxis  --new-window --\"\n"), "/h")
	if err != nil || !reflect.DeepEqual(cfg.Terminal, []string{"ptyxis", "--new-window", "--"}) {
		t.Errorf("terminal = %q, %v", cfg.Terminal, err)
	}
	if cfg, _ := Parse(nil, "/h"); cfg.Terminal != nil {
		t.Errorf("unset: %q", cfg.Terminal)
	}
	if _, err := Parse([]byte("[global]\nterminal = \" \"\n"), "/h"); err == nil || !strings.Contains(err.Error(), "global.terminal: empty") {
		t.Errorf("empty: %v", err)
	}
}

// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package main

import (
	"bytes"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/fqazzazee/shuttle/internal/daemon"
)

// The task Task Scheduler is given: the user's sign-in starts the console-
// less daemon, as them, without raised rights, for as long as it runs, and
// again after a failure.
func TestTaskXML(t *testing.T) {
	def, err := taskXML(`C:\Program Files\shuttle\shuttlew.exe`, "daemon", "Runs the daemon.", `PC\user`)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(def, []byte{0xff, 0xfe}) || len(def)%2 != 0 {
		t.Fatalf("not UTF-16 with its byte order mark: % x", def[:4])
	}
	units := make([]uint16, (len(def)-2)/2)
	for i := range units {
		units[i] = uint16(def[2+2*i]) | uint16(def[3+2*i])<<8
	}
	text := string(utf16.Decode(units))
	var back task
	if err := xml.Unmarshal([]byte(strings.Replace(text, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)), &back); err != nil {
		t.Fatalf("does not read back: %v\n%s", err, text)
	}
	switch {
	case !back.Triggers.Logon.Enabled || back.Triggers.Logon.UserID != `PC\user`:
		t.Errorf("trigger = %+v, want the user's sign-in", back.Triggers.Logon)
	case back.Principals.Principal.LogonType != "InteractiveToken" || back.Principals.Principal.RunLevel != "LeastPrivilege":
		t.Errorf("principal = %+v, want the signed-in user, no raised rights", back.Principals.Principal)
	case back.Actions.Exec.Command != `C:\Program Files\shuttle\shuttlew.exe` || back.Actions.Exec.Arguments != "daemon":
		t.Errorf("action = %+v", back.Actions.Exec)
	case back.Settings.ExecutionTimeLimit != "PT0S" || back.Settings.RestartOnFailure.Count == 0 ||
		back.Settings.DisallowStartIfOnBatteries || back.Settings.StopIfGoingOnBatteries:
		t.Errorf("settings = %+v, want no time limit, restarts, on battery too", back.Settings)
	}
}

func TestAutostartArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"maybe"}, {"on", "off"}} {
		var out, errs bytes.Buffer
		if code := runAutostart(args, &out, &errs); code != 2 || !strings.Contains(errs.String(), "on, off or status") {
			t.Errorf("autostart %q = %d, %q", args, code, errs.String())
		}
	}
}

// Without a console (shuttlew.exe), the daemon logs to daemon.log beside
// the state; with one, to it.
func TestLogToWithoutAConsole(t *testing.T) {
	dir := t.TempDir()
	paths := daemon.Paths{StateDir: filepath.Join(dir, "shuttle", "state")}
	out, closeLog, err := logTo(os.NewFile(0, "no console"), paths, "daemon.log")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("daemon started\n")); err != nil {
		t.Fatal(err)
	}
	closeLog()
	if b, err := os.ReadFile(filepath.Join(dir, "shuttle", "daemon.log")); err != nil || string(b) != "daemon started\n" {
		t.Errorf("daemon.log = %q, %v", b, err)
	}
	var buf bytes.Buffer
	if out, _, _ := logTo(&buf, paths, "daemon.log"); out != &buf {
		t.Error("a writer given is not the one written to")
	}
}

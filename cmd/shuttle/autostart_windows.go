// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package main

import (
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// The daemon starts at sign-in through a Task Scheduler task of the user's
// own: not a Windows service, which would run outside their session, with
// neither their Credential Manager nor their notifications (DECISIONS
// 2026-10-16). It runs shuttlew.exe, this program built without a console,
// so that no window opens at sign-in.

// windowless is the console-less build's name, beside shuttle.exe.
const windowless = "shuttlew.exe"

// runAutostart turns starting the daemon at sign-in on or off, or says
// whether it is on.
func runAutostart(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "shuttle autostart: say on, off or status")
		return 2
	}
	name, err := taskName()
	if err == nil {
		switch args[0] {
		case "on":
			err = autostartOn(name, stdout)
		case "off":
			err = autostartOff(name, stdout)
		case "status":
			autostartStatus(name, stdout)
		default:
			fmt.Fprintf(stderr, "shuttle autostart: %q: say on, off or status\n", args[0])
			return 2
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "shuttle autostart %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

// taskName is the user's task: Task Scheduler's names are the machine's, so
// it carries the user's SID.
func taskName() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("who this user is: %w", err)
	}
	return "shuttle-" + user.User.Sid.String(), nil
}

func autostartOn(name string, stdout io.Writer) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	daemon := filepath.Join(filepath.Dir(self), windowless)
	if _, err := os.Stat(daemon); err != nil {
		return fmt.Errorf("%s is not beside %s: it is what starts at sign-in, without a window", windowless, filepath.Base(self))
	}
	user, err := currentUser()
	if err != nil {
		return err
	}
	def, err := taskXML(daemon, user)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "shuttle-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(def)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := schtasks("/Create", "/TN", name, "/XML", f.Name(), "/F"); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "on: %s daemon starts when you sign in (Task Scheduler: %s)\n", daemon, name)
	return nil
}

func autostartOff(name string, stdout io.Writer) error {
	if !taskExists(name) {
		fmt.Fprintln(stdout, "off already")
		return nil
	}
	if err := schtasks("/Delete", "/TN", name, "/F"); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "off: the daemon no longer starts when you sign in; a running one runs on")
	return nil
}

func autostartStatus(name string, stdout io.Writer) {
	if !taskExists(name) {
		fmt.Fprintln(stdout, "off")
		return
	}
	fmt.Fprintf(stdout, "on (Task Scheduler: %s)\n", name)
}

func taskExists(name string) bool {
	return exec.Command("schtasks", "/Query", "/TN", name).Run() == nil //nolint:gosec // Windows' own tool; the name is ours
}

// schtasks runs Task Scheduler's own tool, with its words as the error.
func schtasks(args ...string) error {
	out, err := exec.Command("schtasks", args...).CombinedOutput() //nolint:gosec // Windows' own tool; the arguments are ours
	if err != nil {
		return fmt.Errorf("schtasks %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}

// currentUser is the user as Task Scheduler names them: DOMAIN\name.
func currentUser() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	account, domain, _, err := user.User.Sid.LookupAccount("")
	if err != nil {
		return "", err
	}
	return domain + `\` + account, nil
}

// task is the part of Task Scheduler's task definition shuttle sets.
type task struct {
	XMLName  xml.Name `xml:"Task"`
	Version  string   `xml:"version,attr"`
	Xmlns    string   `xml:"xmlns,attr"`
	Info     taskInfo `xml:"RegistrationInfo"`
	Triggers struct {
		Logon struct {
			Enabled bool   `xml:"Enabled"`
			UserID  string `xml:"UserId"`
		} `xml:"LogonTrigger"`
	} `xml:"Triggers"`
	Principals struct {
		Principal struct {
			ID        string `xml:"id,attr"`
			UserID    string `xml:"UserId"`
			LogonType string `xml:"LogonType"`
			RunLevel  string `xml:"RunLevel"`
		} `xml:"Principal"`
	} `xml:"Principals"`
	Settings taskSettings `xml:"Settings"`
	Actions  struct {
		Context string `xml:"Context,attr"`
		Exec    struct {
			Command   string `xml:"Command"`
			Arguments string `xml:"Arguments"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

type taskInfo struct {
	Description string `xml:"Description"`
}

type taskSettings struct {
	MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
	DisallowStartIfOnBatteries bool   `xml:"DisallowStartIfOnBatteries"`
	StopIfGoingOnBatteries     bool   `xml:"StopIfGoingOnBatteries"`
	ExecutionTimeLimit         string `xml:"ExecutionTimeLimit"`
	RestartOnFailure           struct {
		Interval string `xml:"Interval"`
		Count    int    `xml:"Count"`
	} `xml:"RestartOnFailure"`
}

// taskXML is the task that runs daemon at user's sign-in, as Task
// Scheduler reads it: UTF-16 with its byte order mark. It runs as the user,
// only while they are signed in, without raised rights; restarts a minute
// after a failure, three times; and runs for as long as it does (Task
// Scheduler stops a task after three days, unless told otherwise), on
// battery too.
func taskXML(daemon, user string) ([]byte, error) {
	var t task
	t.Version, t.Xmlns = "1.2", "http://schemas.microsoft.com/windows/2004/02/mit/task"
	t.Info.Description = "Runs shuttle's daemon, which syncs your shuttles, when you sign in."
	t.Triggers.Logon.Enabled, t.Triggers.Logon.UserID = true, user
	p := &t.Principals.Principal
	p.ID, p.UserID, p.LogonType, p.RunLevel = "Author", user, "InteractiveToken", "LeastPrivilege"
	t.Settings.MultipleInstancesPolicy = "IgnoreNew"
	t.Settings.ExecutionTimeLimit = "PT0S"
	t.Settings.RestartOnFailure.Interval, t.Settings.RestartOnFailure.Count = "PT1M", 3
	t.Actions.Context = "Author"
	t.Actions.Exec.Command, t.Actions.Exec.Arguments = daemon, "daemon"
	body, err := xml.MarshalIndent(&t, "", "  ")
	if err != nil {
		return nil, err
	}
	text := `<?xml version="1.0" encoding="UTF-16"?>` + "\n" + string(body) + "\n"
	out := []byte{0xff, 0xfe}
	for _, u := range utf16.Encode([]rune(text)) {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return out, nil
}

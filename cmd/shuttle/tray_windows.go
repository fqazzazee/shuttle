// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"

	"github.com/fqazzazee/shuttle/internal/bus"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/terminal"
	"github.com/fqazzazee/shuttle/internal/tray"
)

// runTray shows shuttle's icon in the notification area until the user
// quits it. One per user: a second one leaves quietly.
func runTray(ctx context.Context, stderr io.Writer) int {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		fmt.Fprintf(stderr, "shuttle tray: %v\n", err)
		return 1
	}
	out, closeLog, err := logTo(stderr, paths, "tray.log")
	if err != nil {
		fmt.Fprintf(stderr, "shuttle tray: %v\n", err)
		return 1
	}
	defer closeLog()
	log := slog.New(slog.NewTextHandler(out, nil))
	release, err := oneTray()
	if errors.Is(err, errTrayRunning) {
		log.Info("the icon is shown already")
		return 0
	}
	if err != nil {
		log.Error("the icon", "error", err)
		return 1
	}
	defer release()
	self, err := os.Executable()
	if err != nil {
		log.Error("finding shuttle", "error", err)
		return 1
	}
	dir := filepath.Dir(self)
	log.Info("showing the icon")
	tray.Run(ctx, tray.Actions{
		Open:  func(tab string) { openTUI(filepath.Join(dir, "shuttle.exe"), tab, log) },
		Start: func() error { return launchDaemon(filepath.Join(dir, windowless)) },
		Dir:   filepath.Dir(paths.StateDir),
	}, log)
	log.Info("the icon is gone")
	return 0
}

var errTrayRunning = errors.New("the icon is shown already")

// oneTray holds this user's icon: a mutex of theirs only another tray of
// theirs would ask for.
func oneTray() (release func(), err error) {
	pipe, err := bus.PipeName()
	if err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(`Local\shuttle-tray-` + filepath.Base(pipe))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(h) //nolint:errcheck // leaving
		return nil, errTrayRunning
	}
	if err != nil {
		return nil, err
	}
	return func() { windows.CloseHandle(h) }, nil //nolint:errcheck // leaving
}

// openTUI opens shuttle's TUI on tab ("" for the first) in a new terminal
// window: Windows Terminal, or a console of its own.
func openTUI(console, tab string, log *slog.Logger) {
	argv, err := terminal.Command(nil, exec.LookPath, console, "open", tab)
	if err == nil {
		err = terminal.Start(argv, exec.LookPath)
	}
	if err != nil {
		log.Warn("opening shuttle in a terminal", "error", err)
	}
}

// launchDaemon starts the console-less daemon, on its own: closing the icon
// does not stop it.
func launchDaemon(daemonExe string) error {
	cmd := exec.Command(daemonExe, "daemon") //nolint:gosec // shuttle's own, beside this program
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

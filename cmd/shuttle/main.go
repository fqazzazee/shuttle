// SPDX-License-Identifier: GPL-2.0-or-later

// Command shuttle pushes, pulls and syncs folders between SMB/NFS shares and
// local storage. `shuttle daemon` runs the background service; with no
// arguments it will open the TUI (a later phase).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"

	"github.com/charmbracelet/x/term"
	"github.com/godbus/dbus/v5"

	"github.com/fqazzazee/shuttle/internal/bus"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/notify"
	"github.com/fqazzazee/shuttle/internal/secrets"
)

// exitConfig is EX_CONFIG from sysexits.h: the systemd unit does not
// restart on it, since a broken config stays broken until fixed.
const exitConfig = 78

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// SIGINT/SIGTERM end the daemon gently: the file being copied finishes.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		// The TUI, in a terminal; scripts and pipes get the usage.
		if f, ok := stdout.(*os.File); ok && term.IsTerminal(f.Fd()) && term.IsTerminal(os.Stdin.Fd()) {
			return runTUI(ctx, stderr)
		}
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(stdout, "shuttle %s\n", buildVersion())
		return 0
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	case "daemon":
		if err := runDaemon(ctx, stderr); err != nil {
			fmt.Fprintf(stderr, "shuttle daemon: %v\n", err)
			if errors.Is(err, daemon.ErrConfig) {
				return exitConfig
			}
			return 1
		}
		return 0
	case "status", "sync", "pause", "resume", "enable", "disable", "activity":
		return runClient(ctx, args[0], args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "shuttle: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

// buildVersion falls back to the module version so `go install ...@vX.Y.Z`
// reports something useful without ldflags.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

// runDaemon runs every shuttle in the user's config, and serves the D-Bus
// interface once their state is held and shows desktop notifications, until
// ctx is done.
func runDaemon(ctx context.Context, stderr io.Writer) error {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	notifier := notify.New(log)
	d, err := daemon.New(&daemon.Options{Paths: paths, Store: secrets.SecretService(), Log: log, OnEvent: notifier.Send})
	if err != nil {
		return err
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connecting to the session bus: %w", err)
	}
	defer conn.Close()

	var notifying sync.WaitGroup
	defer notifying.Wait() // after cancel, before conn closes
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	notifying.Go(func() { notifier.Run(ctx, conn) })
	runErr := make(chan error, 1)
	go func() { runErr <- d.Run(ctx) }()
	select {
	case err := <-runErr: // failed before it held the state, e.g. already running
		return err
	case <-d.Ready():
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- bus.Serve(ctx, conn, d) }()
	log.Info("serving D-Bus", "name", bus.Name)

	// Whichever ends first ends the other: a daemon nobody can reach, or a
	// bus name with no daemon behind it, is no use.
	select {
	case err := <-serveErr:
		cancel()
		return errors.Join(err, <-runErr)
	case err := <-runErr:
		cancel()
		return errors.Join(err, <-serveErr)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage: shuttle [<command>]

Without a command, in a terminal: the TUI.

Commands:
  status            Show every shuttle's state
  sync <name>       Sync a shuttle now
  pause [<name>]    Pause a shuttle, or all of them
  resume [<name>]   Resume a shuttle, or all of them
  enable <name>     Turn a shuttle on (saved in the config file)
  disable <name>    Turn a shuttle off (saved in the config file)
  activity [<name>] Show what a shuttle, or every shuttle, did lately
  daemon            Run the background service (normally started by systemd)
  version           Print the version
  help              Show this help
`)
}

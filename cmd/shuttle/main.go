// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Command shuttle pushes, pulls and syncs folders between SMB/NFS shares and
// local storage. `shuttle daemon` runs the background service; with no
// arguments it will open the TUI (a later phase).
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/term"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/present"
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
	inTerminal := func() bool {
		f, ok := stdout.(*os.File)
		return ok && term.IsTerminal(f.Fd()) && term.IsTerminal(os.Stdin.Fd())
	}
	if len(args) == 0 {
		// The TUI, in a terminal; scripts and pipes get the usage.
		if inTerminal() {
			return runTUI(ctx, "", stderr)
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
	case "open":
		if len(args) > 2 {
			fmt.Fprintf(stderr, "shuttle open: wrong arguments\n\n")
			usage(stderr)
			return 2
		}
		if !inTerminal() {
			fmt.Fprintln(stderr, "shuttle open: the TUI needs a terminal")
			return 2
		}
		return runTUI(ctx, strings.Join(args[1:], ""), stderr)
	case "tray":
		return runTray(ctx, stderr)
	case "autostart":
		return runAutostart(args[1:], stdout, stderr)
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

func usage(w io.Writer) {
	fmt.Fprintln(w, strings.Join(present.Bus, "\n")+"\n")
	fmt.Fprint(w, `Usage: shuttle [<command>]

Without a command, in a terminal: the TUI.

Commands:
  open [<tab>]      Open the TUI on a tab: shuttles, conflicts, activity,
                    profiles or shares
  status            Show every shuttle's state
  sync <name>       Sync a shuttle now
  pause [<name>]    Pause a shuttle, or all of them
  resume [<name>]   Resume a shuttle, or all of them
  enable <name>     Turn a shuttle on (saved in the config file)
  disable <name>    Turn a shuttle off (saved in the config file)
  activity [<name>] Show what a shuttle, or every shuttle, did lately
  daemon            Run the background service (normally started by systemd)
  tray              Show shuttle's icon in the notification area (Windows)
  autostart on|off|status
                    Start the daemon when you sign in (Windows), or say how
  version           Print the version
  help              Show this help
`)
}

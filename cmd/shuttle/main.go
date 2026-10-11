// SPDX-License-Identifier: GPL-2.0-or-later

// Command shuttle pushes, pulls and syncs folders between SMB/NFS shares and
// local storage. `shuttle daemon` runs the background service; with no
// arguments it will open the TUI (a later phase).
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/secrets"
)

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
			return 1
		}
		return 0
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

// runDaemon runs every shuttle in the user's config until ctx is done.
func runDaemon(ctx context.Context, stderr io.Writer) error {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))
	d, err := daemon.New(&daemon.Options{Paths: paths, Store: secrets.SecretService(), Log: log})
	if err != nil {
		return err
	}
	return d.Run(ctx)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Usage: shuttle <command>

Commands:
  daemon    Run the background service (normally started by systemd)
  version   Print the version
  help      Show this help
`)
}

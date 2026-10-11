// SPDX-License-Identifier: GPL-2.0-or-later

// Command shuttle pushes, pulls and syncs folders between SMB/NFS shares and
// local storage. With no arguments it will open the TUI; `shuttle daemon`
// will run the background service. Both arrive in later phases.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
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
	fmt.Fprint(w, `Usage: shuttle <command>

Commands:
  version   Print the version
  help      Show this help
`)
}

// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fqazzazee/shuttle/internal/bus"
	"github.com/fqazzazee/shuttle/internal/config"
	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/fsys"
	"github.com/fqazzazee/shuttle/internal/present"
	"github.com/fqazzazee/shuttle/internal/secrets"
	"github.com/fqazzazee/shuttle/internal/shares"
	"github.com/fqazzazee/shuttle/internal/tui"
)

// callTimeout bounds one CLI request to the daemon.
const callTimeout = 10 * time.Second

// runClient runs a command that talks to the daemon: status, sync, pause,
// resume, enable, disable and activity. It returns the process exit code.
func runClient(ctx context.Context, cmd string, args []string, stdout, stderr io.Writer) int {
	var name string
	switch {
	case cmd == "status" && len(args) == 0:
	case (cmd == "sync" || cmd == "enable" || cmd == "disable") && len(args) == 1:
		name = args[0]
	case (cmd == "pause" || cmd == "resume" || cmd == "activity") && len(args) <= 1:
		if len(args) == 1 {
			name = args[0]
		}
	default:
		fmt.Fprintf(stderr, "shuttle %s: wrong arguments\n\n", cmd)
		usage(stderr)
		return 2
	}

	c, err := bus.Connect()
	if err != nil && (cmd == "enable" || cmd == "disable") {
		return setEnabledOffline(name, cmd == "enable", stdout, stderr)
	}
	if err != nil {
		fmt.Fprintf(stderr, "shuttle %s: connecting to the session bus: %v\n", cmd, err)
		return 1
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	switch cmd {
	case "status":
		err = status(ctx, c, stdout)
	case "sync":
		if err = c.SyncNow(ctx, name); err == nil {
			fmt.Fprintf(stdout, "syncing %s\n", name)
		}
	case "activity":
		err = printActivity(ctx, c, name, stdout)
	case "enable", "disable":
		err = c.SetEnabled(ctx, name, cmd == "enable")
		if errors.Is(err, bus.ErrNotRunning) {
			return setEnabledOffline(name, cmd == "enable", stdout, stderr)
		}
		if err == nil {
			fmt.Fprintf(stdout, "%sd %s\n", cmd, name)
		}
	case "pause", "resume":
		if err = c.SetPaused(ctx, name, cmd == "pause"); err == nil {
			done := map[string]string{"pause": "paused", "resume": "resumed"}[cmd]
			fmt.Fprintf(stdout, "%s %s\n", done, orElse(name, "all shuttles"))
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "shuttle %s: %v\n", cmd, err)
		return 1
	}
	return 0
}

// setEnabledOffline edits the config file itself when no daemon is there to
// do it: the change applies when the daemon starts.
func setEnabledOffline(name string, on bool, stdout, stderr io.Writer) int {
	cmd := map[bool]string{true: "enable", false: "disable"}[on]
	paths, err := daemon.DefaultPaths()
	if err == nil {
		var home string
		if home, err = os.UserHomeDir(); err == nil {
			_, err = config.SetEnabled(paths.Config, home, name, on)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "shuttle %s: %v\n", cmd, err)
		return 1
	}
	fmt.Fprintf(stdout, "%sd %s (the daemon is not running: it applies when the daemon starts)\n", cmd, name)
	return 0
}

// activityShown is how many entries shuttle activity prints.
const activityShown = 50

// printActivity prints the newest entries of the activity log, oldest
// first, as a log reads.
func printActivity(ctx context.Context, c *bus.Client, name string, w io.Writer) error {
	es, err := c.Activity(ctx, name, activityShown)
	if err != nil {
		return err
	}
	if len(es) == 0 {
		fmt.Fprintln(w, "Nothing yet: entries appear as shuttles copy, delete or run into problems.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for i := len(es) - 1; i >= 0; i-- {
		e := &es[i]
		what := e.Text
		if e.Path != "" {
			what = e.Path + ": " + e.Text
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Time.Local().Format("2006-01-02 15:04:05"), e.Shuttle, what)
	}
	return tw.Flush()
}

func orElse(s, otherwise string) string {
	if s == "" {
		return otherwise
	}
	return s
}

// status prints one line per shuttle.
func status(ctx context.Context, c *bus.Client, w io.Writer) error {
	all, err := c.Shuttles(ctx)
	if err != nil {
		return err
	}
	overall, paused, err := c.State(ctx)
	if err != nil {
		return err
	}
	if paused {
		fmt.Fprintln(w, "All shuttles are paused (shuttle resume).")
	}
	if msg, err := c.ConfigError(ctx); err == nil && msg != "" {
		fmt.Fprintf(w, "The config file has an error, so the previous settings are in use:\n%s\n\n", msg)
	}
	if len(all) == 0 {
		fmt.Fprintln(w, "No shuttles in the config.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tMODE\tSTATE\tSHARE\tSIZE\tFILES\tLAST RUN\tDETAILS")
	now := time.Now()
	for i := range all {
		st := &all[i]
		size, files := present.Synced(st)
		fmt.Fprintf(tw, "%s\t%s %s\t%s\t%s\t%s\t%s\t%s\t%s\n", st.Name, present.ModeArrow(st.Mode), st.Mode, st.State,
			orElse(st.Share, "-"), size, files, present.Ago(st.LastRun, now), details(st, now))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if overall != daemon.StateIdle && overall != daemon.StatePaused && overall != daemon.StateRunning {
		fmt.Fprintf(w, "\nNeeds attention: %s.\n", overall)
	}
	return nil
}

// details is what a status line adds after the columns: the run in
// progress, then whatever needs the user's attention, then when the
// schedule runs it next.
func details(st *daemon.Status, now time.Time) string {
	var parts []string
	for _, p := range []string{present.Progress(&st.Progress), present.Attention(st)} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if next := present.Until(st.NextRun, now); next != "" && st.State != daemon.StateRunning {
		parts = append(parts, "next run "+next)
	}
	return strings.Join(parts, "; ")
}

// runTUI runs the TUI against the daemon on the session bus, on the named
// tab ("" for the first).
func runTUI(ctx context.Context, tab string, stderr io.Writer) int {
	first, ok := tui.Tab(cmp.Or(tab, "shuttles"))
	if !ok {
		fmt.Fprintf(stderr, "shuttle open: no tab %q: shuttles, conflicts, activity, profiles or shares\n", tab)
		return 2
	}
	c, err := bus.Connect()
	if err != nil {
		fmt.Fprintf(stderr, "shuttle: connecting to the session bus: %v\n", err)
		return 1
	}
	defer c.Close()
	paths, err := daemon.DefaultPaths()
	if err != nil {
		fmt.Fprintf(stderr, "shuttle: %v\n", err)
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "shuttle: %v\n", err)
		return 1
	}
	o := tui.Options{Config: paths.Config, Home: home, Store: secrets.SecretService(), Test: shares.Test, Tab: first,
		Browse: func(ctx context.Context, share *config.Share, store secrets.Store) (fsys.FS, error) {
			return shares.OpenShare(ctx, share, ".", store)
		}}
	if err := tui.Run(ctx, c, o); err != nil {
		fmt.Fprintf(stderr, "shuttle: %v\n", err)
		return 1
	}
	return 0
}

// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tray

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"fyne.io/systray"

	"github.com/fqazzazee/shuttle/internal/bus"
	"github.com/fqazzazee/shuttle/internal/daemon"
)

// every is how often the icon asks the daemon how it is.
const every = 2 * time.Second

// Actions are what the menu does beyond asking the daemon: open the TUI,
// start a daemon when none runs.
type Actions struct {
	Open  func()
	Start func() error
}

// Run shows the icon until ctx is done or the user quits it. It must run
// on the program's main goroutine: the icon's window lives on that thread.
func Run(ctx context.Context, a Actions, log *slog.Logger) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	systray.Run(func() { go loop(ctx, a, log) }, cancel)
}

type menu struct {
	status, open, sync, pause, start, quit *systray.MenuItem
}

func loop(ctx context.Context, a Actions, log *slog.Logger) {
	icons := map[look][]byte{}
	for _, l := range []look{lookIdle, lookSyncing, lookPaused, lookError, lookStopped} {
		b, err := iconFor(l)
		if err != nil {
			log.Error("drawing the icon", "error", err)
			systray.Quit()
			return
		}
		icons[l] = b
	}
	m := menu{
		status: systray.AddMenuItem("shuttle", ""),
		open:   systray.AddMenuItem("Open shuttle", "The shuttles, their conflicts and activity, in a terminal"),
	}
	m.status.Disable()
	systray.AddSeparator()
	m.sync = systray.AddMenuItem("Sync now", "Run every shuttle now")
	m.pause = systray.AddMenuItem("Pause all", "Hold every shuttle until resumed")
	m.start = systray.AddMenuItem("Start the daemon", "Start syncing: the daemon is not running")
	systray.AddSeparator()
	m.quit = systray.AddMenuItem("Quit the icon", "The daemon goes on syncing")
	systray.SetOnTapped(a.Open)

	var c *bus.Client
	defer func() {
		if c != nil {
			c.Close()
		}
	}()
	var paused bool
	var all []daemon.Status
	shown := look(-1)
	refresh := func() {
		running := c != nil
		if c == nil {
			var err error
			if c, err = bus.Connect(); err == nil {
				running = true
			}
		}
		var configErr string
		if running {
			ask, done := context.WithTimeout(ctx, every)
			var err error
			_, paused, err = c.State(ask)
			if err == nil {
				all, err = c.Shuttles(ask)
			}
			if err == nil {
				configErr, err = c.ConfigError(ask)
			}
			done()
			if err != nil {
				c.Close()
				c, running = nil, false
			}
		}
		l, words := summary(running, all, paused, configErr)
		if l != shown {
			systray.SetIcon(icons[l])
			shown = l
		}
		systray.SetTooltip(words)
		m.status.SetTitle(words)
		show(m.sync, running)
		show(m.pause, running)
		show(m.start, !running)
		if paused {
			m.pause.SetTitle("Resume all")
		} else {
			m.pause.SetTitle("Pause all")
		}
	}
	refresh()

	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			systray.Quit()
			return
		case <-m.quit.ClickedCh:
			systray.Quit()
			return
		case <-tick.C:
		case <-m.open.ClickedCh:
			a.Open()
		case <-m.start.ClickedCh:
			if err := a.Start(); err != nil {
				log.Error("starting the daemon", "error", err)
			}
		case <-m.sync.ClickedCh:
			if c != nil {
				for i := range all {
					switch all[i].State {
					case daemon.StateDisabled, daemon.StatePaused:
						continue
					}
					if err := c.SyncNow(ctx, all[i].Name); err != nil && !errors.Is(err, daemon.ErrDisabled) {
						log.Warn("syncing", "shuttle", all[i].Name, "error", err)
					}
				}
			}
		case <-m.pause.ClickedCh:
			if c != nil {
				if err := c.SetPaused(ctx, "", !paused); err != nil {
					log.Warn("pausing", "error", err)
				}
			}
		}
		refresh()
	}
}

func show(item *systray.MenuItem, on bool) {
	if on {
		item.Show()
	} else {
		item.Hide()
	}
}

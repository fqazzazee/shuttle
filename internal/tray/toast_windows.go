// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tray

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.sr.ht/~jackmordaunt/go-toast/v2"

	"github.com/fqazzazee/shuttle/internal/bus"
	"github.com/fqazzazee/shuttle/internal/notice"
)

// Windows knows shuttle's toasts by these: the name they show under, and
// the class their buttons call back. The GUID was made once for shuttle and
// must not change: Windows has it in the user's registry.
const (
	appID        = "shuttle"
	activatorID  = "{747DE1F3-C6B1-444A-87F1-ADE3907F4099}"
	openPrefix   = "open:"
	reconnectGap = 5 * time.Second
)

// setUpToasts tells Windows about shuttle's toasts (in the user's registry,
// under Software\Classes\AppUserModelId\shuttle): its name and icon, and
// that a button's click comes back to this process, which opens the TUI on
// the button's tab. iconDir is where the icon's file goes.
func setUpToasts(a Actions, iconDir string) (icon string, err error) {
	icon = filepath.Join(iconDir, "shuttle.png")
	b, err := icons.ReadFile("icons/shuttle-64.png")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(iconDir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(icon, b, 0o600); err != nil {
		return "", err
	}
	if err := toast.SetAppData(toast.AppData{AppID: appID, GUID: activatorID, IconPath: icon}); err != nil {
		return "", err
	}
	toast.SetActivationCallback(func(args string, _ []toast.UserData) {
		if tab, ok := strings.CutPrefix(args, openPrefix); ok {
			a.Open(tab)
		}
	})
	return icon, nil
}

// showNotes shows the daemon's notes as toasts until ctx is done, watching
// again whenever the daemon comes back.
func showNotes(ctx context.Context, icon string, log *slog.Logger) {
	for {
		if c, err := bus.Connect(); err == nil {
			if notes, err := c.Notes(ctx); err == nil {
				for n := range notes {
					t := toastOf(&n, icon)
					if err := t.Push(); err != nil {
						log.Warn("showing a notification", "error", err, "summary", n.Summary)
					}
				}
			}
			c.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectGap):
		}
	}
}

// toastOf is the toast for n: its summary over its body, and, where it
// waits for the user, a button opening the TUI on its tab.
func toastOf(n *notice.Note, icon string) toast.Notification {
	t := toast.Notification{AppID: appID, Title: n.Summary, Body: n.Body, Icon: icon}
	if n.Open != "" {
		t.ActivationArguments = openPrefix + n.Open
		t.Actions = []toast.Action{{Type: toast.Foreground, Content: "Open shuttle", Arguments: openPrefix + n.Open}}
	}
	return t
}

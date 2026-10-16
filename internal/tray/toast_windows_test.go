// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tray

import (
	"testing"

	"github.com/fqazzazee/shuttle/internal/notice"
)

// A note's toast: its summary over its body, under shuttle's name and icon;
// one that waits for the user opens the TUI on its tab, from a button or a
// click on the toast.
func TestToastOf(t *testing.T) {
	plain := toastOf(&notice.Note{Kind: notice.Completed, Summary: "docs: synced", Body: "1 copied"}, `C:\icon.png`)
	if plain.AppID != appID || plain.Title != "docs: synced" || plain.Body != "1 copied" || plain.Icon != `C:\icon.png` ||
		len(plain.Actions) != 0 || plain.ActivationArguments != "" {
		t.Errorf("a plain note = %+v", plain)
	}
	held := toastOf(&notice.Note{Kind: notice.Conflicts, Summary: "docs: 2 conflicts", Open: "conflicts"}, "")
	if len(held.Actions) != 1 || held.Actions[0].Content != "Open shuttle" || held.Actions[0].Arguments != "open:conflicts" ||
		held.ActivationArguments != "open:conflicts" {
		t.Errorf("a note that waits for the user = %+v", held)
	}
}

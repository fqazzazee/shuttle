// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/fqazzazee/shuttle/internal/config"
)

// refusedSummary renders a run's refused names as "op path: reason" lines.
func refusedSummary(res *Result) string {
	var b strings.Builder
	for _, r := range res.Refused {
		b.WriteString(r.Op.String() + " " + r.Path + ": " + r.Reason + "\n")
	}
	return b.String()
}

func TestRefusedNamesAreLeftOut(t *testing.T) {
	shareHoldsWhatNTFSCannot(t)
	h := newPhoneHarness(t, config.ModeSync, config.DeletionsMirror)
	write(t, h.remoteDir, "ok.txt", "ok", t0)
	write(t, h.remoteDir, "a:b.txt", "colon", t0)
	write(t, h.remoteDir, "dir?/x.txt", "x", t0)
	write(t, h.remoteDir, "dir?/sub/y.txt", "y", t0)
	want := "copy-to-local a:b.txt: the local folder cannot hold this name: it contains ':'\n" +
		"mkdir-local dir?: the local folder cannot hold this name: it contains '?'\n"

	for i := range 2 { // left out every run, not an error that fails one
		res := h.run()
		if got := refusedSummary(res); got != want {
			t.Errorf("run %d refused:\n%s\nwant:\n%s", i, got, want)
		}
		if i == 1 && res.Planned != 0 {
			t.Errorf("second run planned %d actions", res.Planned)
		}
	}
	if got := listing(t, h.localDir); got != "ok.txt=ok" {
		t.Errorf("phone = %s", got)
	}

	actions, err := Preview(context.Background(), &h.opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 0 {
		t.Errorf("preview = %s, want nothing to do", summary(actions))
	}
}

func TestRefusedNamesAreNeverDeletedOnTheShare(t *testing.T) {
	shareHoldsWhatNTFSCannot(t)
	h := newPhoneHarness(t, config.ModePush, config.DeletionsMirror)
	h.opts.AllowDeletions = true // nothing for the guard to catch: the run itself must not delete
	write(t, h.localDir, "ok.txt", "ok", t0)
	write(t, h.remoteDir, "a:b.txt", "colon", t0)
	write(t, h.remoteDir, "dir?/x.txt", "x", t0)
	write(t, h.remoteDir, "gone.txt", "mirrored away", t0)

	res := h.run()
	if res.Deleted != 1 || len(res.Refused) != 2 {
		t.Errorf("result = %+v, want gone.txt deleted and two names left out", res)
	}
	if got := listing(t, h.remoteDir); got != "a:b.txt=colon dir?/ dir?/x.txt=x ok.txt=ok" {
		t.Errorf("share = %s", got)
	}
}

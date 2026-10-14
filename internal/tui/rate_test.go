// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/engine"
)

// running is docs mid-run, at rate bytes a second.
func running(rate int64) changedMsg {
	return changedMsg{Name: "docs", State: daemon.StateRunning,
		Progress: daemon.Progress{Phase: engine.PhaseSyncing, Done: 1, Total: 4, RateUp: rate}}
}

// at sets the model's clock to now and a number of half seconds.
func at(m *model, halves int) {
	m.now = func() time.Time { return now.Add(time.Duration(halves) * sampleEvery) }
}

// While a shuttle runs, the panel draws its rate over the last half
// minute, the highest full, with the rate now and the most it was. When
// the run ends, the rate goes, and the next run starts afresh.
func TestRateSparkline(t *testing.T) {
	m := start(t, newFake(), 120, 30)
	for i, rate := range []int64{1_000_000, 2_000_000, 4_000_000, 8_000_000, 0} {
		at(m, i)
		send(t, m, running(rate))
	}
	if s := screen(m); !strings.Contains(s, "Rate            ▁▂▄█   0 B/s · at most 8.0 MB/s") {
		t.Errorf("no sparkline:\n%s", s)
	}
	if !strings.Contains(m.View().Content, sparkStyles[7].Render("█")) || sparkStyles[0].GetForeground() == sparkStyles[7].GetForeground() {
		t.Error("the sparkline's heights are not in the gradient's colors")
	}
	send(t, m, changedMsg{Name: "docs", State: daemon.StateIdle})
	if s := screen(m); strings.Contains(s, "Rate  ") || len(m.rates) != 0 {
		t.Errorf("the rate outlived the run: %v\n%s", m.rates, s)
	}
}

// A sample comes a half second; one sooner replaces the last, and a
// refresh soon after a signal adds none of its own.
func TestRateSamples(t *testing.T) {
	m := start(t, newFake(), 120, 30)
	at(m, 0)
	send(t, m, running(1000))
	m.now = func() time.Time { return now.Add(sampleEvery / 4) }
	send(t, m, running(2000))
	if got := m.rates["docs"]; len(got) != 1 || got[0].rate != 2000 {
		t.Errorf("a sample sooner than a half second: %+v", got)
	}
	at(m, 1)
	send(t, m, running(3000))
	m.now = func() time.Time { return now.Add(sampleEvery + sampleEvery*3/2) }
	st := daemon.Status(running(3000))
	m.Update(refreshedMsg{shuttles: []daemon.Status{st}})
	if got := m.rates["docs"]; len(got) != 2 {
		t.Errorf("a refresh after a signal added a sample: %+v", got)
	}
	for i := range 2 * rateSamples {
		at(m, 10+i)
		send(t, m, running(int64(i+1)))
	}
	if got := m.rates["docs"]; len(got) != rateSamples || got[len(got)-1].rate != 2*rateSamples {
		t.Errorf("%d samples kept, the last %+v; want the last %d", len(got), got[len(got)-1], rateSamples)
	}
}

// The sparkline gives way in a narrow panel, the latest samples kept.
func TestRateLineFits(t *testing.T) {
	m := start(t, newFake(), 120, 30)
	for i := range rateSamples {
		at(m, i)
		send(t, m, running(int64(1000*(i+1))))
	}
	for _, width := range []int{80, 40, 20} {
		line := m.rateLine("docs", width)
		if w := ansi.StringWidth(line); w > width {
			t.Errorf("width %d: %d columns: %q", width, w, ansi.Strip(line))
		}
		if !strings.HasSuffix(strings.SplitN(ansi.Strip(line), "  ", 2)[0], "█") {
			t.Errorf("width %d: the latest, highest sample is not last: %q", width, ansi.Strip(line))
		}
	}
}

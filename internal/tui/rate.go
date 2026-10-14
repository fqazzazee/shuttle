// SPDX-License-Identifier: GPL-2.0-or-later

package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/fqazzazee/shuttle/internal/daemon"
	"github.com/fqazzazee/shuttle/internal/present"
)

// sampleEvery is how often a run's rate is kept for its sparkline: as
// often as the daemon reports a run's progress. rateSamples is how many
// are kept, the last half minute.
const (
	sampleEvery = 500 * time.Millisecond
	rateSamples = 60
)

// rateSample is a run's transfer rate, both ways together, at a time.
type rateSample struct {
	at   time.Time
	rate int64
}

// sample keeps st's transfer rate while it runs, one a sampleEvery: one
// closer than within to the last replaces it. A run that ended forgets its
// own, so the next starts afresh.
func (m *model) sample(st *daemon.Status, within time.Duration) {
	if st.Progress.Phase == "" {
		delete(m.rates, st.Name)
		return
	}
	s := rateSample{m.now(), st.Progress.RateUp + st.Progress.RateDown}
	all := m.rates[st.Name]
	if n := len(all); n > 0 && s.at.Sub(all[n-1].at) < within {
		all[n-1] = s
		return
	}
	if len(all) == rateSamples {
		all = append(all[:0], all[1:]...)
	}
	m.rates[st.Name] = append(all, s)
}

// rateLine is a run's rate over the last half minute as a sparkline, in
// at most width columns, then the rate now and the most it was; "" until
// there are two samples to draw.
func (m *model) rateLine(name string, width int) string {
	all := m.rates[name]
	if len(all) < 2 {
		return ""
	}
	most := int64(0)
	for _, s := range all {
		most = max(most, s.rate)
	}
	// The most it was goes first, then the sparkline, where the room is
	// short for them.
	text := "  " + present.Size(all[len(all)-1].rate) + "/s"
	if long := text + " · at most " + present.Size(most) + "/s"; width-ansi.StringWidth(long) >= sparkLeast {
		text = long
	}
	cols := width - ansi.StringWidth(text)
	if cols < 2 {
		return strings.TrimLeft(text, " ")
	}
	return sparkline(all[max(len(all)-cols, 0):], most) + text
}

// sparkLeast is the fewest columns a sparkline is drawn in beside the most
// a rate was.
const sparkLeast = 8

// sparkLevels are a column of a sparkline from low to high; nothing at
// all is a space.
var sparkLevels = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

// sparkline is the samples as columns of a height for their rate, the
// highest, most, full; each in its height's color along the gradient.
func sparkline(all []rateSample, most int64) string {
	var b strings.Builder
	for _, s := range all {
		if s.rate <= 0 || most <= 0 {
			b.WriteString(" ")
			continue
		}
		level := min(int((s.rate*int64(len(sparkLevels))-1)/most), len(sparkLevels)-1)
		b.WriteString(sparkStyles[level].Render(sparkLevels[level]))
	}
	return b.String()
}

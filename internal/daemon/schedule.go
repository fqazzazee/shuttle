// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package daemon

import (
	"time"

	"github.com/fqazzazee/shuttle/internal/config"
)

// recheckEvery is the longest the scheduler sleeps before reading the wall
// clock again. Go's timers do not count time spent suspended, so a long sleep
// would fire late after the laptop wakes; re-reading the clock catches that.
var recheckEvery = time.Minute

// nextDue returns when a shuttle with a schedule trigger is next due, given
// when it last ran (zero: never). ok is false without a schedule.
//
//   - interval: lastRun + interval, or now if the shuttle never ran.
//   - at: the next of the day's times after now. A time that passed since
//     lastRun (the machine was off or asleep) makes the shuttle due now, once.
//
// With both, the earlier wins.
func nextDue(s *config.Settings, lastRun, now time.Time) (time.Time, bool) {
	if !s.Has(config.TriggerSchedule) {
		return time.Time{}, false
	}
	var next time.Time
	pick := func(t time.Time) {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	if s.Interval > 0 {
		if lastRun.IsZero() {
			pick(now)
		} else {
			pick(lastRun.Add(s.Interval))
		}
	}
	for _, at := range s.At {
		if !lastRun.IsZero() && lastRun.Before(prevSlot(at, now)) {
			pick(now) // missed while off or asleep
		}
		pick(nextSlot(at, now))
	}
	if next.IsZero() {
		return time.Time{}, false
	}
	if next.Before(now) {
		next = now
	}
	return next, true
}

// nextSlot is the first time of day at strictly after now, in now's zone.
func nextSlot(at config.TimeOfDay, now time.Time) time.Time {
	t := slotOn(now, at, 0)
	if !t.After(now) {
		t = slotOn(now, at, 1)
	}
	return t
}

// prevSlot is the last time of day at at or before now.
func prevSlot(at config.TimeOfDay, now time.Time) time.Time {
	t := slotOn(now, at, 0)
	if t.After(now) {
		t = slotOn(now, at, -1)
	}
	return t
}

// slotOn is at on the day days after now's date. On the day clocks spring
// forward, at may not exist (02:30 when 02:00 jumps to 03:00); time.Date then
// picks an instant near it (Go leaves which one unspecified), so the run
// happens that day, once, near the requested time. TestNextDue checks that.
func slotOn(now time.Time, at config.TimeOfDay, days int) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+days, at.Hour, at.Minute, 0, 0, now.Location())
}

// setNextRun shows when the schedule runs the shuttle next. The scheduler
// wakes every recheckEvery: only a change goes out.
func (r *runner) setNextRun(t time.Time) {
	r.mu.Lock()
	same := r.st.NextRun.Equal(t)
	r.st.NextRun = t
	r.mu.Unlock()
	if !same {
		r.changed()
	}
}

// schedule triggers the runner whenever its schedule is due, until quit.
func (r *runner) schedule(quit <-chan struct{}) {
	for {
		now := r.d.o.Now()
		next, ok := nextDue(&r.shuttle.Settings, r.lastAttempt(), now)
		if !ok {
			return
		}
		if !next.After(now) {
			r.markAttempt(now) // so the same slot is not triggered twice
			r.trigger()
			continue
		}
		r.setNextRun(next)
		wait := min(next.Sub(now), recheckEvery)
		select {
		case <-quit:
			return
		case <-time.After(wait):
		}
	}
}

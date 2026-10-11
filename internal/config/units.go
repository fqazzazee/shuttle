// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Duration is a time.Duration written as "90s", "30m", "12h", "30d" or "2w".
// Days and weeks are added because retention periods are counted in them.
type Duration time.Duration

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := parseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	for suffix, unit := range map[string]time.Duration{"d": 24 * time.Hour, "w": 7 * 24 * time.Hour} {
		if num, ok := strings.CutSuffix(s, suffix); ok {
			n, err := strconv.ParseFloat(num, 64)
			if err != nil || n < 0 || math.IsInf(n, 0) || n*float64(unit) > math.MaxInt64 {
				return 0, fmt.Errorf("invalid duration %q", s)
			}
			return time.Duration(n * float64(unit)), nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid duration %q: use e.g. 90s, 30m, 12h, 30d, 2w", s)
	}
	return d, nil
}

// ByteSize is a size written as "500MB", "20GB" or "1.5GiB". Decimal units
// (kB, MB, GB, TB) are powers of 1000; binary units (KiB, MiB, GiB, TiB) are
// powers of 1024; a bare number is bytes.
type ByteSize int64

var sizeUnits = []struct {
	suffix string
	mult   float64
}{
	// Longest suffixes first, so "GiB" is not read as "B".
	{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"TiB", 1 << 40},
	{"kB", 1e3}, {"KB", 1e3}, {"MB", 1e6}, {"GB", 1e9}, {"TB", 1e12},
	{"B", 1},
}

func (b *ByteSize) UnmarshalText(text []byte) error {
	v, err := parseByteSize(string(text))
	if err != nil {
		return err
	}
	*b = ByteSize(v)
	return nil
}

func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	num, mult := s, 1.0
	for _, u := range sizeUnits {
		if n, ok := strings.CutSuffix(s, u.suffix); ok {
			num, mult = strings.TrimSpace(n), u.mult
			break
		}
	}
	n, err := strconv.ParseFloat(num, 64)
	if err != nil || n < 0 || n*mult > math.MaxInt64 {
		return 0, fmt.Errorf("invalid size %q: use e.g. 500MB, 20GB, 1.5GiB", s)
	}
	return int64(n * mult), nil
}

// Rate is a transfer rate in bytes per second, written as a size per second:
// "20MB/s", "512KiB/s". Zero means unlimited.
type Rate int64

func (r *Rate) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	size, ok := strings.CutSuffix(s, "/s")
	if !ok {
		return fmt.Errorf("invalid rate %q: use a size per second, e.g. 20MB/s", s)
	}
	v, err := parseByteSize(size)
	if err != nil {
		return fmt.Errorf("invalid rate %q: use a size per second, e.g. 20MB/s", s)
	}
	*r = Rate(v)
	return nil
}

// TimeOfDay is a local wall-clock time written as "HH:MM".
type TimeOfDay struct {
	Hour, Minute int
}

func (t *TimeOfDay) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	parsed, err := time.Parse("15:04", s)
	if err != nil {
		return fmt.Errorf("invalid time %q: use 24-hour HH:MM, e.g. 02:00", s)
	}
	*t = TimeOfDay{Hour: parsed.Hour(), Minute: parsed.Minute()}
	return nil
}

func (t TimeOfDay) String() string {
	return fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
}

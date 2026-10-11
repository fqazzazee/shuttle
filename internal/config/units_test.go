// SPDX-License-Identifier: GPL-2.0-or-later

package config

import (
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	ok := map[string]time.Duration{
		"90s": 90 * time.Second, "30m": 30 * time.Minute, "1h30m": 90 * time.Minute,
		"30d": 30 * day, "1.5d": 36 * time.Hour, "2w": 14 * day, "0d": 0,
	}
	for in, want := range ok {
		var d Duration
		if err := d.UnmarshalText([]byte(in)); err != nil || time.Duration(d) != want {
			t.Errorf("Duration(%q) = %v, %v; want %v", in, time.Duration(d), err, want)
		}
	}
	for _, in := range []string{"", "30", "d", "-1d", "-5m", "1y", "abc"} {
		var d Duration
		if err := d.UnmarshalText([]byte(in)); err == nil {
			t.Errorf("Duration(%q) = %v, want an error", in, time.Duration(d))
		}
	}
}

func TestByteSize(t *testing.T) {
	ok := map[string]int64{
		"0": 0, "512": 512, "512B": 512, "1kB": 1000, "1KB": 1000, "1KiB": 1024,
		"20GB": 20e9, "1.5GiB": 3 << 29, "2 TB": 2e12, "1MiB": 1 << 20,
	}
	for in, want := range ok {
		var b ByteSize
		if err := b.UnmarshalText([]byte(in)); err != nil || int64(b) != want {
			t.Errorf("ByteSize(%q) = %d, %v; want %d", in, b, err, want)
		}
	}
	for _, in := range []string{"", "GB", "-1MB", "1XB", "lots", "1e30TB"} {
		var b ByteSize
		if err := b.UnmarshalText([]byte(in)); err == nil {
			t.Errorf("ByteSize(%q) = %d, want an error", in, b)
		}
	}
}

func TestRate(t *testing.T) {
	var r Rate
	if err := r.UnmarshalText([]byte("20MB/s")); err != nil || r != 20e6 {
		t.Errorf("Rate(20MB/s) = %d, %v", r, err)
	}
	for _, in := range []string{"20MB", "fast/s", "/s"} {
		if err := r.UnmarshalText([]byte(in)); err == nil {
			t.Errorf("Rate(%q) succeeded, want an error", in)
		}
	}
}

func TestTimeOfDay(t *testing.T) {
	var tod TimeOfDay
	if err := tod.UnmarshalText([]byte("02:05")); err != nil || tod != (TimeOfDay{2, 5}) || tod.String() != "02:05" {
		t.Errorf("TimeOfDay(02:05) = %v, %v", tod, err)
	}
	for _, in := range []string{"24:00", "2pm", "12:60", ""} {
		if err := tod.UnmarshalText([]byte(in)); err == nil {
			t.Errorf("TimeOfDay(%q) succeeded, want an error", in)
		}
	}
}

// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package network

import (
	"context"
	"testing"
	"time"
)

func TestIsMetered(t *testing.T) {
	for _, tt := range []struct {
		cost uint32
		want bool
	}{
		{0, false},            // unknown
		{0x1, false},          // unrestricted
		{0x2, true},           // fixed: Settings' "metered connection"
		{0x4, true},           // variable
		{0x1 | 0x40000, true}, // unrestricted, but roaming
		{0x2 | 0x10000, true},
		{0x20000, false}, // congested, no more
	} {
		if got := isMetered(tt.cost); got != tt.want {
			t.Errorf("isMetered(%#x) = %v, want %v", tt.cost, got, tt.want)
		}
	}
}

// Watch asks this machine's Network List Manager at once, says what it
// heard, and stops with its context.
func TestWatchAsksWindows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	heard := make(chan bool, 4)
	done := make(chan error, 1)
	go func() { done <- Watch(ctx, func(m bool) { heard <- m }) }()
	select {
	case m := <-heard:
		t.Logf("this connection metered: %v", m)
	case <-time.After(time.Second):
		t.Fatal("Watch said nothing at first")
	}
	if err := <-done; err != nil {
		t.Fatalf("Watch = %v", err)
	}
}

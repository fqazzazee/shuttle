// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package network

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/windows"
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
	// Windows Server, as on CI's runner, may have no Network List Manager:
	// then Watch says "not metered" first, and why it stopped.
	if err := <-done; err != nil && !errors.Is(err, classNotRegistered) {
		t.Fatalf("Watch = %v", err)
	} else if err != nil {
		t.Logf("no Network List Manager here: %v", err)
	}
}

// classNotRegistered is REGDB_E_CLASSNOTREG.
const classNotRegistered = windows.Errno(0x80040154)

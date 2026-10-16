// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package network

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows' Network List Manager, and its INetworkCostManager: what the
// connection to the internet costs.
var (
	clsidNetworkListManager = windows.GUID{Data1: 0xDCB00C01, Data2: 0x570F, Data3: 0x4A9B,
		Data4: [8]byte{0x8D, 0x69, 0x19, 0x9F, 0xDB, 0xA5, 0x72, 0x3B}}
	iidNetworkCostManager = windows.GUID{Data1: 0xDCB00008, Data2: 0x570F, Data3: 0x4A9B,
		Data4: [8]byte{0x8D, 0x69, 0x19, 0x9F, 0xDB, 0xA5, 0x72, 0x3B}}
	coCreateInstance = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")
)

// NLM_CONNECTION_COST: how a connection is paid for, and how close it is to
// its limit.
const (
	costFixed         = 0x2     // a plan with a limit: what "metered connection" in Settings sets
	costVariable      = 0x4     // paid by the byte
	costOverDataLimit = 0x10000 // past the plan's limit
	costRoaming       = 0x40000 // away from the plan's network
)

// clsctxAll is CLSCTX_INPROC_SERVER|CLSCTX_INPROC_HANDLER|CLSCTX_LOCAL_SERVER|CLSCTX_REMOTE_SERVER.
const clsctxAll = 0x17

// pollEvery is how often Windows is asked: it has no change event this
// side of a COM event sink, and the question is cheap.
var pollEvery = 30 * time.Second

// isMetered says whether a connection that costs cost is metered: one with
// a plan's limit, paid by the byte, past its limit or roaming. Unknown, or
// unrestricted, is not: a shuttle that pauses for nothing is worse than one
// that runs.
func isMetered(cost uint32) bool {
	return cost&(costFixed|costVariable|costOverDataLimit|costRoaming) != 0
}

// Watch calls onChange with whether the connection is metered, at once and
// then each time that changes, until ctx is done. Windows is asked every
// pollEvery. Without its Network List Manager, the connection is not
// metered.
func Watch(ctx context.Context, onChange func(metered bool)) error {
	// COM is set up per thread: this goroutine keeps its own.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil && !errors.Is(err, windows.Errno(1)) { // S_FALSE: set up already
		onChange(false)
		return fmt.Errorf("network: COM: %w", err)
	}
	defer windows.CoUninitialize()
	m, err := newCostManager()
	if err != nil {
		onChange(false)
		return err
	}
	defer m.release()

	metered := m.metered()
	onChange(metered)
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if now := m.metered(); now != metered {
				metered = now
				onChange(metered)
			}
		}
	}
}

// costManager is an INetworkCostManager: IUnknown's three methods, then
// GetCost, GetDataPlanStatus and SetDestinationAddresses.
type costManager struct{ vtbl *costManagerVtbl }

type costManagerVtbl struct {
	QueryInterface, AddRef, Release             uintptr
	GetCost, GetDataPlanStatus, SetDestinations uintptr
}

func newCostManager() (*costManager, error) {
	var m *costManager
	hr, _, _ := coCreateInstance.Call(uintptr(unsafe.Pointer(&clsidNetworkListManager)), 0, clsctxAll, //nolint:gosec // COM: pointers it takes
		uintptr(unsafe.Pointer(&iidNetworkCostManager)), uintptr(unsafe.Pointer(&m))) //nolint:gosec // COM: pointers it takes
	if hr != 0 || m == nil {
		return nil, fmt.Errorf("network: the Network List Manager: %w", windows.Errno(hr))
	}
	return m, nil
}

// metered asks what the connection to the internet costs: not metered, when
// Windows cannot say.
func (m *costManager) metered() bool {
	var cost uint32
	hr, _, _ := syscall.SyscallN(m.vtbl.GetCost, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(&cost)), 0) //nolint:gosec // COM: a vtable call
	return hr == 0 && isMetered(cost)
}

func (m *costManager) release() {
	syscall.SyscallN(m.vtbl.Release, uintptr(unsafe.Pointer(m))) //nolint:errcheck,gosec // COM: the count left tells nothing to act on
}

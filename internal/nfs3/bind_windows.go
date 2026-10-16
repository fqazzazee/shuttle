// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package nfs3

import (
	"errors"

	"golang.org/x/sys/windows"
)

// mayNotBind says err is this process not being allowed the port. Windows
// keeps no ports for administrators; a firewall or another program's
// exclusive use can still refuse one.
func mayNotBind(err error) bool {
	return errors.Is(err, windows.WSAEACCES)
}

// portTaken says err is a local port already in use, or in TIME_WAIT with
// this server. Winsock reports these as its own errors, not as syscall's
// EADDRINUSE and EADDRNOTAVAIL.
func portTaken(err error) bool {
	return errors.Is(err, windows.WSAEADDRINUSE) || errors.Is(err, windows.WSAEADDRNOTAVAIL)
}

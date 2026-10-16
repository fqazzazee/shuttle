// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package nfs3

import (
	"errors"
	"syscall"
)

// mayNotBind says err is this process not being allowed a reserved port.
func mayNotBind(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}

// portTaken says err is a local port already in use, or in TIME_WAIT with
// this server.
func portTaken(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.EADDRNOTAVAIL)
}

// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

//go:build !windows

package fsys

// OSNames are the rules of a local folder here: none stricter than Linux's.
// A folder whose file system has stricter ones, such as Android's shared
// storage, is opened with them (OpenLocalWith).
var OSNames = Names{}

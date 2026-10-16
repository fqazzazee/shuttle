// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package network tells whether the computer's connection is metered: a
// phone's hotspot, a mobile plan, or a connection the user marked so. On
// Linux, as NetworkManager has it on the system bus; on Windows, as its
// Network List Manager does.
//
// This package is desktop-specific; the sync core must not import it.
package network

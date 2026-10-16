// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package bus is how the daemon talks to its clients: the CLI, the TUI, and
// the GNOME extension or the tray. On Linux it is D-Bus (SPEC.md, "D-Bus
// API"); on Windows, a named pipe of this user's, carrying JSON, with the
// same Client API (DECISIONS 2026-10-16).
//
// This package is desktop-specific; the sync core must not import it.
package bus

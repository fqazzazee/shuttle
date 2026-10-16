// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

// Package credman keeps share passwords in Windows' Credential Manager, as
// package secretservice keeps them in the Secret Service on Linux: one generic
// credential per password, named "shuttle:" and the key's account
// ("shuttle:smb://user@nas.example/home"), kept on this machine, never
// roaming. Never plaintext as a fallback.
//
// This package is desktop-specific: only the daemon's desktop side, the CLI
// and the TUI import it (SPEC.md, portability rule). It builds on Windows
// alone.
package credman

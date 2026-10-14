// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.io.File

/**
 * Where the app keeps its own files, all in its private storage: the config the daemon
 * reads, and the state folder with each shuttle's state database, activity log and the
 * daemon's log (DECISIONS 2026-10-14, "Android: config on the phone").
 */
class AppFiles(filesDir: File) {
    val config = File(filesDir, "config.toml")
    val state = File(filesDir, "state")
}

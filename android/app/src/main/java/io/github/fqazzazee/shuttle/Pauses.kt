// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.SharedPreferences
import androidx.core.content.edit
import org.json.JSONArray

/**
 * The user's pauses. The desktop's daemon keeps them while it runs; the app's daemons come and go
 * (the screen's, the service's, each worker's), so the app keeps them and starts every daemon with
 * them in place, before any shuttle can run.
 */
class Pauses(private val prefs: SharedPreferences) {
    val paused: Set<String> get() = prefs.getStringSet(PAUSED, emptySet())!!.toSet()
    val all: Boolean get() = prefs.getBoolean(ALL, false)

    fun set(name: String, paused: Boolean) {
        val now = if (paused) this.paused + name else this.paused - name
        prefs.edit { putStringSet(PAUSED, now) }
    }

    fun setAll(paused: Boolean) = prefs.edit { putBoolean(ALL, paused) }

    /** The paused shuttles for the binding's Options.PausedJSON. */
    fun json(): String = JSONArray(paused.sorted()).toString()

    private companion object {
        const val PAUSED = "paused"
        const val ALL = "paused_all"
    }
}

// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.time.Instant
import java.time.OffsetDateTime
import org.json.JSONObject

/** A run's progress, as the daemon sends it while one is running. */
data class Progress(
    val phase: String,
    val done: Long,
    val total: Long,
    val bytes: Long,
    val totalBytes: Long,
    val path: String,
    val rateUp: Long,
    val rateDown: Long,
) {
    /** How far along, 0 to 1, by bytes when there are some to copy, else by files; null before it is known. */
    val fraction: Float?
        get() = when {
            totalBytes > 0 -> (bytes.toFloat() / totalBytes).coerceIn(0f, 1f)
            total > 0 -> (done.toFloat() / total).coerceIn(0f, 1f)
            else -> null
        }
}

/** One shuttle's status, as the daemon sends it (daemon.Status), with what the list shows. */
data class ShuttleStatus(
    val name: String,
    val mode: String,
    val state: String,
    val paused: Boolean,
    val waiting: String,
    val lastRun: Instant?,
    val error: String,
    val conflicts: Int,
    val errors: Int,
    val refused: Int,
    val progress: Progress?,
) {
    /** The TUI's arrow for the mode: up to the share, down from it, both ways. */
    val arrow: String
        get() = when (mode) {
            "push" -> "↑"
            "pull" -> "↓"
            else -> "⇅"
        }

    companion object {
        fun parse(json: String): ShuttleStatus {
            val o = JSONObject(json)
            fun count(key: String) = o.optJSONArray(key)?.length() ?: 0
            val p = o.optJSONObject("Progress")
            return ShuttleStatus(
                name = o.getString("Name"),
                mode = o.getString("Mode"),
                state = o.getString("State"),
                paused = o.getBoolean("Paused"),
                waiting = o.optString("Waiting"),
                lastRun = time(o.optString("LastRun")),
                error = o.optString("Error"),
                conflicts = count("Conflicts"),
                errors = count("Errors"),
                refused = count("Refused"),
                progress = p?.takeIf { o.getString("State") == "running" }?.let {
                    Progress(
                        phase = it.optString("Phase"),
                        done = it.optLong("Done"),
                        total = it.optLong("Total"),
                        bytes = it.optLong("Bytes"),
                        totalBytes = it.optLong("TotalBytes"),
                        path = it.optString("Path"),
                        rateUp = it.optLong("RateUp"),
                        rateDown = it.optLong("RateDown"),
                    )
                },
            )
        }

        /** Go's time, or null for its zero (the year 1): never. */
        private fun time(s: String): Instant? =
            s.takeIf { it.isNotEmpty() }?.let { OffsetDateTime.parse(it).toInstant() }?.takeIf { it.isAfter(Instant.EPOCH) }
    }
}

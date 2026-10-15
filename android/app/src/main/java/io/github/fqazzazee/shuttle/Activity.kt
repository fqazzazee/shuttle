// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import org.json.JSONArray

/** One line of a shuttle's activity log (the Go core's activity.Entry). */
data class Entry(val time: Instant, val shuttle: String, val kind: String, val path: String, val text: String) {
    /** Something that went wrong or waits for the user, rather than work done. */
    val problem get() = kind in PROBLEMS

    companion object {
        private val PROBLEMS = setOf("error", "conflict", "held", "skipped", "left-out")

        /** The entries as the binding's ActivityJSON writes them, newest first. */
        fun parseAll(json: String): List<Entry> {
            val a = JSONArray(json)
            return (0 until a.length()).map { i ->
                val o = a.getJSONObject(i)
                Entry(OffsetDateTime.parse(o.getString("time")).toInstant(), o.optString("shuttle"), o.getString("kind"),
                    o.optString("path"), o.getString("text"))
            }
        }
    }
}

/** An entry's time as the TUI stamps it: the time alone today, the date too before. */
fun stamp(t: Instant, now: Instant, zone: ZoneId = ZoneId.systemDefault()): String {
    val at = t.atZone(zone)
    val today = now.atZone(zone)
    val pattern = when {
        at.toLocalDate() == today.toLocalDate() -> "HH:mm:ss"
        at.year == today.year -> "MMM d HH:mm:ss"
        else -> "yyyy-MM-dd HH:mm"
    }
    return at.format(DateTimeFormatter.ofPattern(pattern))
}

/**
 * The Activity tab: what the shuttles did and ran into, newest first, for every shuttle or one,
 * and all of it or only the problems. entries is null while the log is being read.
 */
@Composable
fun ActivityScreen(
    shuttles: List<String>,
    entries: List<Entry>?,
    shuttle: String,
    onShuttle: (String) -> Unit,
    problemsOnly: Boolean,
    onProblemsOnly: (Boolean) -> Unit,
    now: Instant = Instant.now(),
) {
    Column(Modifier.safeDrawingPadding().padding(horizontal = 24.dp, vertical = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(R.string.tab_activity), style = MaterialTheme.typography.headlineMedium)
        Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(shuttle.isEmpty(), { onShuttle("") }, { Text(stringResource(R.string.activity_every)) })
            for (s in shuttles) FilterChip(shuttle == s, { onShuttle(s) }, { Text(s) })
        }
        FilterChip(problemsOnly, { onProblemsOnly(!problemsOnly) }, { Text(stringResource(R.string.activity_problems)) })
        val shown = entries?.filter { !problemsOnly || it.problem }
        when {
            shown == null -> Text(stringResource(R.string.activity_reading))
            shown.isEmpty() && entries.isNotEmpty() -> Text(stringResource(R.string.activity_no_problems))
            shown.isEmpty() -> Text(stringResource(R.string.activity_none))
            else -> LazyColumn(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                items(shown) { e -> EntryLine(e, shuttle.isEmpty(), now) }
            }
        }
    }
}

@Composable
private fun EntryLine(e: Entry, withShuttle: Boolean, now: Instant) {
    val color = when (e.kind) {
        "error" -> MaterialTheme.colorScheme.error
        "conflict", "held", "left-out" -> MaterialTheme.colorScheme.tertiary
        "skipped" -> MaterialTheme.colorScheme.onSurfaceVariant
        "run" -> MaterialTheme.colorScheme.primary
        else -> Color.Unspecified
    }
    Column {
        val head = listOfNotNull(stamp(e.time, now), e.shuttle.takeIf { withShuttle && it.isNotEmpty() }).joinToString("  ")
        Text(head, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(if (e.path.isEmpty()) e.text else "${e.path}: ${e.text}", style = MaterialTheme.typography.bodyMedium, color = color)
    }
}

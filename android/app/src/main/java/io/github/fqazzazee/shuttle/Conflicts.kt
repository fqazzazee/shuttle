// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.text.format.Formatter
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import java.time.Instant
import java.time.OffsetDateTime
import org.json.JSONObject

/** One side's copy in a conflict: a file of its size, or a folder; when it last changed. */
data class Copy(val folder: Boolean, val size: Long, val modified: Instant)

/** How a conflict can be settled, as the daemon names it (engine.Choice). */
enum class Keep(val choice: String, val label: Int) {
    LOCAL("keep-local", R.string.keep_local),
    REMOTE("keep-remote", R.string.keep_remote),
    BOTH("keep-both", R.string.keep_both),
}

/** A conflict a shuttle holds for the user, with its two copies as the run found them. */
data class ConflictItem(val shuttle: String, val path: String, val reason: String, val local: Copy?, val remote: Copy?) {
    /**
     * What can be done, as in the TUI: either copy, or both, for two files; both, for a file against a
     * folder; nothing for names that differ only in case, which the user renames.
     */
    val choices: List<Keep>
        get() = when {
            local == null || remote == null -> emptyList()
            local.folder != remote.folder -> listOf(Keep.BOTH)
            else -> Keep.entries
        }

    companion object {
        /** The conflicts held across the shuttles, shuttle by shuttle, from their statuses as JSON. */
        fun held(statuses: Map<String, String>): List<ConflictItem> = statuses.toSortedMap().values.flatMap { json ->
            val st = JSONObject(json)
            val all = st.optJSONArray("Conflicts") ?: return@flatMap emptyList()
            (0 until all.length()).map { all.getJSONObject(it) }.filter { it.optString("Outcome") == "held" }.map { c ->
                ConflictItem(st.getString("Name"), c.getString("Path"), c.optString("Reason"), copy(c.optJSONObject("Local")), copy(c.optJSONObject("Remote")))
            }
        }

        private fun copy(o: JSONObject?) = o?.let {
            Copy(it.optInt("Kind") == 1, it.optLong("Size"), OffsetDateTime.parse(it.getString("ModTime")).toInstant())
        }
    }
}

/** The Conflicts tab: what waits for the user, and how to settle each. resolve runs off the main thread. */
@Composable
fun ConflictsScreen(items: List<ConflictItem>, error: String?, resolve: (ConflictItem, Keep) -> Unit) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.tab_conflicts), style = MaterialTheme.typography.headlineMedium)
        error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        if (items.isEmpty()) Text(stringResource(R.string.conflicts_none))
        for (c in items) ConflictCard(c, resolve)
    }
}

@OptIn(ExperimentalLayoutApi::class) // FlowRow
@Composable
private fun ConflictCard(c: ConflictItem, resolve: (ConflictItem, Keep) -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(c.path, style = MaterialTheme.typography.titleMedium)
            Text("${c.shuttle} · ${c.reason}", style = MaterialTheme.typography.bodyMedium)
            CopyLine(R.string.conflict_phone, c.local)
            CopyLine(R.string.conflict_share, c.remote)
            if (c.choices.isEmpty()) {
                Text(stringResource(R.string.conflict_rename), style = MaterialTheme.typography.bodySmall)
            }
            // Wraps whole buttons onto a new line rather than a label onto two.
            FlowRow(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                for (k in c.choices) TextButton(onClick = { resolve(c, k) }) { Text(stringResource(k.label), maxLines = 1) }
            }
        }
    }
}

@Composable
private fun CopyLine(label: Int, copy: Copy?) {
    val context = LocalContext.current
    val what = when {
        copy == null -> stringResource(R.string.conflict_none_here)
        copy.folder -> stringResource(R.string.conflict_folder)
        else -> Formatter.formatShortFileSize(context, copy.size)
    }
    val justNow = stringResource(R.string.just_now)
    val changed = copy?.let { ", " + ago(it.modified, justNow) } ?: ""
    Text("${stringResource(label)}: $what$changed", style = MaterialTheme.typography.bodySmall)
}

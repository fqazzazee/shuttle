// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.json.JSONArray
import org.json.JSONObject

/** A path and why: a file that could not be synced, or a name left out. */
data class PathIssue(val path: String, val reason: String)

/** The selected shuttle in full, in the TUI panel's words (the binding's DetailJSON). */
data class Detail(
    val name: String,
    val mode: String,
    val arrow: String,
    val profile: String,
    val local: String,
    val remote: String,
    val localRoom: String,
    val remoteRoom: String,
    val localLow: Boolean,
    val remoteLow: Boolean,
    val syncs: String,
    val runs: String,
    val lastRun: String,
    val now: String,
    val waiting: String,
    val attention: String,
    val errors: List<PathIssue>,
    val refused: List<PathIssue>,
    val enabled: Boolean = true,
) {
    companion object {
        fun parse(json: String): Detail {
            val o = JSONObject(json)
            fun issues(key: String) = (o.optJSONArray(key) ?: JSONArray()).let { a ->
                (0 until a.length()).map { a.getJSONObject(it).let { i -> PathIssue(i.getString("Path"), i.optString("Reason")) } }
            }
            return Detail(
                o.getString("Name"), o.getString("Mode"), o.getString("Arrow"), o.optString("Profile"),
                o.optString("Local"), o.optString("Remote"), o.optString("LocalRoom"), o.optString("RemoteRoom"),
                o.optBoolean("LocalLow"), o.optBoolean("RemoteLow"), o.optString("Syncs"), o.optString("Runs"),
                o.optString("LastRun"), o.optString("Now"), o.optString("Waiting"), o.optString("Attention"),
                issues("Errors"), issues("Refused"), o.optBoolean("Enabled", true),
            )
        }
    }
}

/** What the shuttle's screen does; delete returns why it was refused, or null. */
class DetailActions(
    val back: () -> Unit,
    val edit: () -> Unit,
    val settings: () -> Unit,
    val setEnabled: (Boolean) -> Unit,
    val delete: () -> String?,
)

/**
 * The selected shuttle in full, as the TUI's panel: label and value, then what failed and what was
 * left out. Edit opens it in the wizard; Settings, its settings alone. Below, it is turned off or on,
 * or deleted, once asked: its files stay where they are.
 */
@Composable
fun DetailScreen(d: Detail, actions: DetailActions) {
    var deleting by remember { mutableStateOf(false) }
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(Modifier.fillMaxWidth()) {
            TextButton(onClick = actions.back) { Text(stringResource(R.string.back)) }
            Spacer(Modifier.weight(1f))
            TextButton(onClick = actions.edit) { Text(stringResource(R.string.edit)) }
            TextButton(onClick = actions.settings) { Text(stringResource(R.string.settings)) }
        }
        Text("${d.arrow} ${d.name}", style = MaterialTheme.typography.headlineMedium)
        Text(
            listOf(d.mode, d.profile.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.detail_profile, it) })
                .filterNotNull().joinToString(" · "),
            style = MaterialTheme.typography.bodyMedium,
        )
        val warn = MaterialTheme.colorScheme.tertiary
        val bad = MaterialTheme.colorScheme.error
        Line(R.string.detail_phone, withRoom(d.local, d.localRoom), if (d.localLow) warn else null)
        Line(R.string.detail_share, withRoom(d.remote, d.remoteRoom), if (d.remoteLow) warn else null)
        Line(R.string.detail_syncs, d.syncs)
        Line(R.string.detail_runs, d.runs)
        Line(R.string.detail_last_run, d.lastRun)
        Line(R.string.detail_now, d.now)
        Line(R.string.detail_waiting, d.waiting, warn)
        Line(R.string.detail_attention, d.attention, bad)
        Issues(R.string.detail_errors, d.errors)
        Issues(R.string.detail_refused, d.refused)
        HorizontalDivider()
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = { actions.setEnabled(!d.enabled) }) { Text(stringResource(if (d.enabled) R.string.disable else R.string.enable)) }
            OutlinedButton(onClick = { deleting = true }) { Text(stringResource(R.string.delete_shuttle)) }
        }
        if (!d.enabled) Text(stringResource(R.string.detail_disabled), style = MaterialTheme.typography.bodySmall)
    }
    if (deleting) DeleteShuttleDialog(d.name, actions.delete) { deleting = false }
}

@Composable
private fun DeleteShuttleDialog(name: String, delete: () -> String?, close: () -> Unit) {
    var refused by remember { mutableStateOf<String?>(null) }
    AlertDialog(
        onDismissRequest = close,
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.delete_shuttle_ask, name))
                refused?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            TextButton(onClick = {
                refused = delete()
                if (refused == null) close()
            }) { Text(stringResource(R.string.delete)) }
        },
        dismissButton = { TextButton(onClick = close) { Text(stringResource(R.string.add_cancel)) } },
    )
}

private fun withRoom(folder: String, room: String) = if (room.isEmpty()) folder else "$folder\n$room"

@Composable
private fun Line(label: Int, value: String, color: Color? = null) {
    if (value.isEmpty()) return
    Row(Modifier.fillMaxWidth()) {
        Text(stringResource(label), Modifier.width(110.dp), style = MaterialTheme.typography.labelLarge)
        Text(value, style = MaterialTheme.typography.bodyMedium, color = color ?: MaterialTheme.colorScheme.onSurface)
    }
}

@Composable
private fun Issues(title: Int, issues: List<PathIssue>) {
    if (issues.isEmpty()) return
    HorizontalDivider()
    Text(stringResource(title), style = MaterialTheme.typography.titleMedium)
    for (i in issues) {
        Text(i.path, style = MaterialTheme.typography.bodyMedium)
        Text(i.reason, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

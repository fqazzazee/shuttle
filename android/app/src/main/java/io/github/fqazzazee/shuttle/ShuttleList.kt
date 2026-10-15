// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.text.format.Formatter
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/** What the list's buttons do. */
class ListActions(
    val open: (String) -> Unit,
    val syncNow: (String) -> Unit,
    val setPaused: (String, Boolean) -> Unit,
    val setPausedAll: (Boolean) -> Unit,
)

/** The shuttles, as the dashboard lists them: each one's mode, state and progress, with its buttons. */
@Composable
fun ShuttleList(shuttles: List<ShuttleStatus>, pausedAll: Boolean, actions: ListActions) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
        OutlinedButton(onClick = { actions.setPausedAll(!pausedAll) }) {
            Text(stringResource(if (pausedAll) R.string.resume_all else R.string.pause_all))
        }
    }
    for (s in shuttles) ShuttleCard(s, actions)
}

@Composable
private fun ShuttleCard(s: ShuttleStatus, actions: ListActions) {
    Card(onClick = { actions.open(s.name) }, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("${s.arrow} ${s.name}", style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                Text(stateLabel(s), style = MaterialTheme.typography.labelLarge, color = stateColor(s.state))
            }
            Text(detail(s), style = MaterialTheme.typography.bodyMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
            s.progress?.let { p ->
                val f = p.fraction
                if (f != null) {
                    LinearProgressIndicator(progress = { f }, modifier = Modifier.fillMaxWidth())
                } else {
                    LinearProgressIndicator(Modifier.fillMaxWidth())
                }
            }
            problems(s)?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error) }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                if (s.state != "disabled") {
                    TextButton(onClick = { actions.setPaused(s.name, !s.paused) }) {
                        Text(stringResource(if (s.paused) R.string.resume else R.string.pause))
                    }
                    TextButton(onClick = { actions.syncNow(s.name) }) { Text(stringResource(R.string.sync_now)) }
                }
            }
        }
    }
}

@Composable
private fun stateLabel(s: ShuttleStatus): String = stringResource(
    when (s.state) {
        "running" -> R.string.state_running
        "paused" -> if (s.paused || s.waiting.isEmpty()) R.string.state_paused else R.string.state_waiting
        "held" -> R.string.state_held
        "error" -> R.string.state_error
        "offline" -> R.string.state_offline
        "disabled" -> R.string.state_disabled
        else -> R.string.state_idle
    },
)

@Composable
private fun stateColor(state: String) = when (state) {
    "error", "offline" -> MaterialTheme.colorScheme.error
    "held" -> MaterialTheme.colorScheme.tertiary
    "running" -> MaterialTheme.colorScheme.primary
    else -> MaterialTheme.colorScheme.onSurfaceVariant
}

/** The line under the name: what it is doing, why it waits, or when it last ran. */
@Composable
private fun detail(s: ShuttleStatus): String {
    val context = LocalContext.current
    s.progress?.let { p ->
        val rate = maxOf(p.rateUp, p.rateDown)
        val what = p.path.ifEmpty { p.phase }
        return if (rate > 0) {
            stringResource(R.string.progress_rate, what, Formatter.formatShortFileSize(context, rate))
        } else {
            what
        }
    }
    if (s.waiting.isNotEmpty() && !s.paused) return s.waiting
    if (s.error.isNotEmpty()) return s.error
    val last = s.lastRun ?: return stringResource(R.string.never_run)
    return stringResource(R.string.last_run, ago(last, stringResource(R.string.just_now)))
}

/** Conflicts, paths that failed, names left out: what the selected shuttle's panel will show in full. */
@Composable
private fun problems(s: ShuttleStatus): String? {
    val parts = buildList {
        if (s.conflicts > 0) add(pluralStringResource(R.plurals.conflicts, s.conflicts, s.conflicts))
        if (s.errors > 0) add(pluralStringResource(R.plurals.errors, s.errors, s.errors))
        if (s.refused > 0) add(pluralStringResource(R.plurals.left_out, s.refused, s.refused))
    }
    return parts.takeIf { it.isNotEmpty() }?.joinToString(" · ")
}

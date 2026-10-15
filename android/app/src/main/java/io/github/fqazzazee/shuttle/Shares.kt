// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.json.JSONArray

/** A share as the Shares tab lists it (the binding's SharesJSON). */
data class ShareRow(
    val name: String,
    val type: String,
    val where: String,
    val usedBy: List<String>,
    val state: String,
    val room: String,
    val low: Boolean,
) {
    companion object {
        fun parseAll(json: String): List<ShareRow> {
            val a = JSONArray(json)
            return (0 until a.length()).map { i ->
                val o = a.getJSONObject(i)
                val users = o.optJSONArray("UsedBy") ?: JSONArray()
                ShareRow(
                    o.getString("Name"), o.getString("Type"), o.optString("Where"),
                    (0 until users.length()).map { users.getString(it) }, o.optString("State"), o.optString("Room"),
                    o.optBoolean("Low"),
                )
            }
        }
    }
}

/**
 * The Shares tab: each share, where it is, which shuttles use it, whether they reach it, and its
 * room; a share opens its editor, and a new one can be added. note is what the last save or delete
 * came to.
 */
@Composable
fun SharesScreen(shares: List<ShareRow>, note: String?, onOpen: (String) -> Unit, onAdd: () -> Unit) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.tab_shares), style = MaterialTheme.typography.headlineMedium)
        note?.let { Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary) }
        if (shares.isEmpty()) Text(stringResource(R.string.shares_none))
        for (s in shares) ShareCard(s) { onOpen(s.name) }
        OutlinedButton(onClick = onAdd, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.share_add)) }
    }
}

@Composable
private fun ShareCard(s: ShareRow, onOpen: () -> Unit) {
    Card(onClick = onOpen, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(s.name, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                val (state, color) = when (s.state) {
                    "online" -> R.string.share_online to MaterialTheme.colorScheme.primary
                    "offline" -> R.string.share_offline to MaterialTheme.colorScheme.error
                    else -> R.string.share_unknown to MaterialTheme.colorScheme.onSurfaceVariant
                }
                Text(stringResource(state), style = MaterialTheme.typography.labelLarge, color = color)
            }
            Text("${s.type} · ${s.where}", style = MaterialTheme.typography.bodyMedium)
            Text(
                if (s.usedBy.isEmpty()) stringResource(R.string.share_unused) else stringResource(R.string.share_used_by, s.usedBy.joinToString(", ")),
                style = MaterialTheme.typography.bodySmall,
            )
            if (s.room.isNotEmpty()) {
                Text(
                    s.room, style = MaterialTheme.typography.bodySmall,
                    color = if (s.low) MaterialTheme.colorScheme.tertiary else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

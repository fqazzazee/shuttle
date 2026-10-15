// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.json.JSONArray

/** A profile as the Profiles tab lists it (the binding's ProfilesJSON). */
data class ProfileRow(val name: String, val mode: String, val triggers: String, val usedBy: List<String>) {
    companion object {
        fun parseAll(json: String): List<ProfileRow> {
            val a = JSONArray(json)
            return (0 until a.length()).map { i ->
                val o = a.getJSONObject(i)
                val users = o.optJSONArray("UsedBy") ?: JSONArray()
                ProfileRow(o.getString("Name"), o.optString("Mode"), o.optString("Triggers"), (0 until users.length()).map { users.getString(it) })
            }
        }
    }
}

/** The Profiles tab: each profile, its mode and triggers, and which shuttles use it, as the TUI's. */
@Composable
fun ProfilesScreen(profiles: List<ProfileRow>) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.tab_profiles), style = MaterialTheme.typography.headlineMedium)
        if (profiles.isEmpty()) Text(stringResource(R.string.profiles_none))
        for (p in profiles) {
            Card(Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(p.name, style = MaterialTheme.typography.titleMedium)
                    Text(
                        listOf(p.mode.ifEmpty { "—" }, p.triggers).joinToString(" · "),
                        style = MaterialTheme.typography.bodyMedium,
                    )
                    Text(
                        if (p.usedBy.isEmpty()) stringResource(R.string.share_unused) else stringResource(R.string.share_used_by, p.usedBy.joinToString(", ")),
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
            }
        }
    }
}

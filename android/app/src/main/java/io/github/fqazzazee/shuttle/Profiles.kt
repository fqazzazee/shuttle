// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
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

/** What the profile editor's buttons do; each returns why it was refused, or null. */
class ProfileActions(
    val create: (name: String, mode: String) -> String?,
    val duplicate: (from: String, to: String) -> String?,
    val delete: (name: String) -> String?,
)

/**
 * The Profiles tab: each profile, its mode and triggers, and which shuttles use it, as the TUI's; a
 * profile opens its editor, and a new one can be added. note is what the last delete came to.
 */
@Composable
fun ProfilesScreen(profiles: List<ProfileRow>, note: String?, onOpen: (String) -> Unit, onAdd: () -> Unit) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.tab_profiles), style = MaterialTheme.typography.headlineMedium)
        note?.let { Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary) }
        if (profiles.isEmpty()) Text(stringResource(R.string.profiles_none))
        for (p in profiles) {
            Card(onClick = { onOpen(p.name) }, modifier = Modifier.fillMaxWidth()) {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(p.name, style = MaterialTheme.typography.titleMedium)
                    Text(
                        listOf(p.mode.ifEmpty { "—" }, p.triggers).joinToString(" · "),
                        style = MaterialTheme.typography.bodyMedium,
                    )
                    Text(usedBy(p.usedBy), style = MaterialTheme.typography.bodySmall)
                }
            }
        }
        OutlinedButton(onClick = onAdd, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.profile_add)) }
    }
}

/**
 * A profile's editor: its settings, as a shuttle's are edited, where the default applies to what it
 * does not set; and duplicate, and delete for one no shuttle uses (a profile is not renamed).
 */
@Composable
fun ProfileEditor(
    name: String,
    rows: List<Setting>,
    usedBy: List<String>,
    set: (key: String, text: String) -> String?,
    actions: ProfileActions,
    onBack: () -> Unit,
) {
    var copying by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf(false) }
    var refused by remember(name) { mutableStateOf<String?>(null) }
    val head = @Composable {
        Text(usedBy(usedBy), style = MaterialTheme.typography.bodySmall)
        refused?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            TextButton(onClick = { copying = true }) { Text(stringResource(R.string.profile_duplicate)) }
            if (usedBy.isEmpty()) TextButton(onClick = { deleting = true }) { Text(stringResource(R.string.share_delete)) }
        }
    }
    SettingsScreen(stringResource(R.string.profile_title, name), rows, set, head, onBack)
    if (copying) {
        NameDialog(stringResource(R.string.profile_duplicate_title, name), "$name-copy", withMode = false, { to, _ -> actions.duplicate(name, to) }) {
            copying = false
        }
    }
    if (deleting) {
        AlertDialog(
            onDismissRequest = { deleting = false },
            text = { Text(stringResource(R.string.profile_delete_ask, name)) },
            confirmButton = {
                TextButton(onClick = { deleting = false; refused = actions.delete(name) }) { Text(stringResource(R.string.share_delete)) }
            },
            dismissButton = { TextButton(onClick = { deleting = false }) { Text(stringResource(R.string.add_cancel)) } },
        )
    }
}

/** Asks a new profile's name and its mode: a profile that set nothing would not be written. */
@Composable
fun NewProfileDialog(create: (name: String, mode: String) -> String?, close: () -> Unit) {
    NameDialog(stringResource(R.string.profile_new), "", withMode = true, create, close)
}

@Composable
private fun NameDialog(title: String, initial: String, withMode: Boolean, save: (name: String, mode: String) -> String?, close: () -> Unit) {
    var name by remember { mutableStateOf(initial) }
    var mode by remember { mutableStateOf("") }
    var refused by remember { mutableStateOf<String?>(null) }
    AlertDialog(
        onDismissRequest = close,
        title = { Text(title) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { name = it }, label = { Text(stringResource(R.string.profile_name)) }, singleLine = true)
                if (withMode) {
                    for ((m, label) in listOf("push" to R.string.mode_push, "pull" to R.string.mode_pull, "sync" to R.string.mode_sync)) {
                        Row(
                            Modifier.fillMaxWidth().selectable(selected = mode == m) { mode = m },
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            RadioButton(selected = mode == m, onClick = { mode = m })
                            Text(stringResource(label))
                        }
                    }
                    Text(stringResource(R.string.profile_new_hint), style = MaterialTheme.typography.bodySmall)
                }
                refused?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            TextButton(enabled = name.isNotBlank() && (!withMode || mode.isNotEmpty()), onClick = {
                refused = save(name.trim(), mode)
                if (refused == null) close()
            }) { Text(stringResource(R.string.add_save)) }
        },
        dismissButton = { TextButton(onClick = close) { Text(stringResource(R.string.add_cancel)) } },
    )
}

@Composable
private fun usedBy(users: List<String>): String =
    if (users.isEmpty()) stringResource(R.string.share_unused) else stringResource(R.string.share_used_by, users.joinToString(", "))

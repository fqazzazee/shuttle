// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.foundation.clickable
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
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
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

/** One setting of a shuttle, as the TUI's editor and GNOME's preferences have it (the binding's SettingsJSON). */
data class Setting(
    val key: String,
    val label: String,
    val hint: String,
    val choices: List<String>,
    val list: Boolean,
    val value: String,
    val inherited: String,
    val from: String,
    val off: String,
) {
    /** What applies: the shuttle's own, else what it inherits. */
    val shown get() = value.ifEmpty { inherited }

    companion object {
        fun parseAll(json: String): List<Setting> {
            val a = JSONArray(json)
            return (0 until a.length()).map { i ->
                val o = a.getJSONObject(i)
                val c = o.optJSONArray("Choices") ?: JSONArray()
                Setting(
                    o.getString("Key"), o.getString("Label"), o.optString("Hint"),
                    (0 until c.length()).map { c.getString(it) }, o.optBoolean("List"),
                    o.optString("Value"), o.optString("Inherited"), o.optString("From"), o.optString("Off"),
                )
            }
        }
    }
}

/**
 * A shuttle's settings: every row, with what applies and whence. A row is set from its editor, or
 * reset to what the shuttle's profile (or the default) says. set returns why a value was refused,
 * or null.
 */
@Composable
fun SettingsScreen(name: String, rows: List<Setting>, set: (key: String, text: String) -> String?, onBack: () -> Unit) {
    var editing by remember { mutableStateOf<Setting?>(null) }
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        TextButton(onClick = onBack) { Text(stringResource(R.string.back)) }
        Text(stringResource(R.string.settings_title, name), style = MaterialTheme.typography.headlineMedium)
        for (r in rows) {
            val off = r.off.isNotEmpty()
            Column(
                Modifier.fillMaxWidth().clickable(enabled = !off) { editing = r }.padding(vertical = 10.dp),
            ) {
                val dim = MaterialTheme.colorScheme.onSurfaceVariant
                Text(r.label, style = MaterialTheme.typography.titleSmall, color = if (off) dim else MaterialTheme.colorScheme.onSurface)
                val whence = if (r.value.isEmpty()) stringResource(R.string.settings_from, r.from) else stringResource(R.string.settings_own)
                Text("${r.shown.ifEmpty { "—" }}  ·  $whence", style = MaterialTheme.typography.bodyMedium, color = dim)
                if (off) Text(r.off, style = MaterialTheme.typography.bodySmall, color = dim)
            }
            HorizontalDivider()
        }
    }
    editing?.let { r -> EditSetting(r, set) { editing = null } }
}

@Composable
private fun EditSetting(r: Setting, set: (String, String) -> String?, close: () -> Unit) {
    var text by remember { mutableStateOf(r.value.ifEmpty { r.inherited }) }
    var refused by remember { mutableStateOf<String?>(null) }
    fun save(t: String) {
        refused = set(r.key, t)
        if (refused == null) close()
    }
    AlertDialog(
        onDismissRequest = close,
        title = { Text(r.label) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (r.choices.isNotEmpty()) {
                    for (c in r.choices) {
                        Row(
                            Modifier.fillMaxWidth().selectable(selected = text == c) { text = c },
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            RadioButton(selected = text == c, onClick = { text = c })
                            Text(c)
                        }
                    }
                } else {
                    OutlinedTextField(text, { text = it }, singleLine = true)
                }
                if (r.hint.isNotEmpty()) Text(r.hint, style = MaterialTheme.typography.bodySmall)
                refused?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = { TextButton(onClick = { save(text) }) { Text(stringResource(R.string.settings_save)) } },
        dismissButton = {
            Row {
                if (r.value.isNotEmpty()) {
                    TextButton(onClick = { save("") }) { Text(stringResource(R.string.settings_reset, r.from, r.inherited)) }
                }
                TextButton(onClick = close) { Text(stringResource(R.string.add_cancel)) }
            }
        },
    )
}

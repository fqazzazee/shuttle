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
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.json.JSONArray

/** One thing in a folder on a share, as the wizard lists it (the binding's BrowseJSON). */
data class FolderEntry(val name: String, val dir: Boolean) {
    companion object {
        fun parseAll(json: String): List<FolderEntry> {
            val a = JSONArray(json)
            return (0 until a.length()).map { i -> a.getJSONObject(i).let { FolderEntry(it.getString("Name"), it.optBoolean("Dir")) } }
        }
    }
}

/** What a listing came to: what is in the folder, or why it could not be read. */
data class Listing(val entries: List<FolderEntry>?, val error: String?)

enum class WizardStep { SHARE, FOLDER, LATER }

/** Where the wizard is: its step, the share chosen, and the folder on it, a slash path from its top. */
data class WizardState(val step: WizardStep = WizardStep.SHARE, val share: String = "", val dir: String = "/") {
    /** The folder as a shuttle's remote reads: "nas:/Photos". */
    val remote get() = "$share:$dir"

    fun into(name: String) = copy(dir = if (dir == "/") "/$name" else "$dir/$name")

    fun up() = copy(dir = dir.substringBeforeLast('/').ifEmpty { "/" })
}

/** The wizard's first step: the share the shuttle syncs with. */
@Composable
fun WizardShareStep(shares: List<ShareRow>, chosen: String, onChoose: (String) -> Unit, onNext: () -> Unit, onCancel: () -> Unit) {
    WizardPage(stringResource(R.string.wizard_share_title)) {
        if (shares.isEmpty()) Text(stringResource(R.string.wizard_share_none))
        for (s in shares) {
            Row(
                Modifier.fillMaxWidth().selectable(selected = s.name == chosen) { onChoose(s.name) }.padding(vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                RadioButton(selected = s.name == chosen, onClick = { onChoose(s.name) })
                Column {
                    Text(s.name, style = MaterialTheme.typography.titleMedium)
                    Text("${s.type} · ${s.where}", style = MaterialTheme.typography.bodySmall)
                }
            }
        }
        Buttons(onCancel, stringResource(R.string.add_cancel), stringResource(R.string.wizard_next), enabled = chosen.isNotEmpty(), onNext)
    }
}

/**
 * The wizard's second step: the shuttle's folder on the share, browsed from its top. listing is
 * null while the folder is read; a folder that could not be read can be tried again, and is not
 * chosen.
 */
@Composable
fun WizardFolderStep(
    state: WizardState,
    listing: Listing?,
    onOpen: (WizardState) -> Unit,
    onRetry: () -> Unit,
    onBack: () -> Unit,
    onNext: () -> Unit,
) {
    WizardPage(stringResource(R.string.wizard_folder_title)) {
        Text(state.remote, style = MaterialTheme.typography.bodyLarge)
        if (state.dir != "/") FolderRow(stringResource(R.string.wizard_up)) { onOpen(state.up()) }
        when {
            listing == null -> Text(stringResource(R.string.wizard_reading))
            listing.entries == null -> {
                Text(listing.error.orEmpty(), color = MaterialTheme.colorScheme.error)
                TextButton(onClick = onRetry) { Text(stringResource(R.string.wizard_retry)) }
            }
            listing.entries.isEmpty() -> Text(stringResource(R.string.wizard_empty))
            else -> for (e in listing.entries) {
                // Files show what is in the folder; only folders open.
                if (e.dir) FolderRow(e.name + "/") { onOpen(state.into(e.name)) } else FolderRow(e.name, null)
            }
        }
        Buttons(onBack, stringResource(R.string.wizard_back), stringResource(R.string.wizard_use), enabled = listing?.entries != null, onNext)
    }
}

/** Until the rest of the wizard is built: the folder chosen, and how to add the shuttle meanwhile. */
@Composable
fun WizardLater(remote: String, onClose: () -> Unit) {
    WizardPage(stringResource(R.string.wizard_later_title)) {
        Text(stringResource(R.string.wizard_later, remote))
        Buttons(null, "", stringResource(R.string.ok), enabled = true, onClose)
    }
}

@Composable
private fun WizardPage(title: String, content: @Composable () -> Unit) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(stringResource(R.string.wizard_add), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary)
        Text(title, style = MaterialTheme.typography.headlineMedium)
        content()
    }
}

@Composable
private fun FolderRow(text: String, onClick: (() -> Unit)?) {
    Text(
        text,
        Modifier.fillMaxWidth().clickable(enabled = onClick != null) { onClick?.invoke() }.padding(vertical = 12.dp),
        style = MaterialTheme.typography.bodyLarge,
        color = if (onClick != null) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
    )
    HorizontalDivider()
}

@Composable
private fun Buttons(onBack: (() -> Unit)?, back: String, next: String, enabled: Boolean, onNext: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(top = 8.dp), horizontalArrangement = Arrangement.End) {
        onBack?.let { TextButton(onClick = it) { Text(back) } }
        Button(enabled = enabled, onClick = onNext) { Text(next) }
    }
}

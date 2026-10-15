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
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TriStateCheckbox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.state.ToggleableState
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

enum class WizardStep { SHARE, FOLDER, WHAT, LOCAL, PROFILE, SETTINGS }

/**
 * Where the wizard is: its step, the share chosen, the folder on it (a slash path from its top),
 * the folder inside that shown while marking what to sync, the folder on the phone (a full path;
 * "" until that step first shows), the shuttle's name, and its profile ("" for none).
 */
data class WizardState(
    val step: WizardStep = WizardStep.SHARE,
    val share: String = "",
    val dir: String = "/",
    val inner: String = "/",
    val local: String = "",
    val name: String = "",
    val profile: String = "",
) {
    /** The folder as a shuttle's remote reads: "nas:/Photos". */
    val remote get() = "$share:$dir"

    fun into(name: String) = copy(dir = child(dir, name))

    fun up() = copy(dir = parent(dir))
}

/** The slash path of name in the folder dir. */
fun child(dir: String, name: String) = if (dir == "/") "/$name" else "$dir/$name"

/** Whether the slash path p is top or inside it. */
fun within(p: String, top: String) = top == "/" || p == top || p.startsWith("$top/")

/** The folder the slash path p is in. */
fun parent(p: String) = p.substringBeforeLast('/').ifEmpty { "/" }

/** A place the phone keeps shared files: its own storage, or an SD card. */
data class Volume(val label: String, val path: String)

/**
 * Whether p is the top of the phone's storage, or above it: a shuttle there would sync all of it,
 * and the app's own folders, so the wizard does not choose it.
 */
fun storageTop(p: String, volumes: List<Volume>) = p.trimEnd('/') in AddForm.STORAGE_ROOTS || volumes.any { it.path == p }

/** What the wizard's marks mean, as the binding's Picker has them (PickAll and so on), with their words. */
val pickModes = listOf(0 to R.string.pick_all, 1 to R.string.pick_include, 2 to R.string.pick_exclude)

/** The wizard's first step, as the TUI's: the shuttle's name, and the share it syncs with. problem is why Next did not go on. */
@Composable
fun WizardShareStep(
    shares: List<ShareRow>,
    chosen: String,
    name: String,
    problem: String?,
    onName: (String) -> Unit,
    onChoose: (String) -> Unit,
    onNext: () -> Unit,
    onCancel: () -> Unit,
) {
    WizardPage(stringResource(R.string.wizard_share_title)) {
        OutlinedTextField(name, onName, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.wizard_name)) }, singleLine = true)
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
        Problem(problem)
        Buttons(onCancel, stringResource(R.string.add_cancel), stringResource(R.string.wizard_next), enabled = chosen.isNotEmpty() && name.isNotBlank(), onNext)
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

/** What the wizard's third step shows of the marks: what they mean, how much of each path they sync, and the marks. */
class MarksView(val mode: Int, val synced: (String) -> Int, val marks: List<String>)

/**
 * The wizard's third step: what the shuttle syncs of its folder, everything, only what is marked,
 * or all but what is marked, as the TUI's tree marks it. The folder is browsed as before, from the
 * shuttle's folder down; a tick marks a file or a folder. Only what is marked needs a mark to go on.
 */
@Composable
fun WizardWhatStep(
    state: WizardState,
    listing: Listing?,
    view: MarksView,
    onMode: (Int) -> Unit,
    onToggle: (String) -> Unit,
    onOpen: (WizardState) -> Unit,
    onRetry: () -> Unit,
    onBack: () -> Unit,
    onNext: () -> Unit,
) {
    WizardPage(stringResource(R.string.wizard_what_title)) {
        Text(state.remote, style = MaterialTheme.typography.bodyLarge)
        for ((m, label) in pickModes) {
            Row(
                Modifier.fillMaxWidth().selectable(selected = view.mode == m) { onMode(m) },
                verticalAlignment = Alignment.CenterVertically,
            ) {
                RadioButton(selected = view.mode == m, onClick = { onMode(m) })
                Text(stringResource(label))
            }
        }
        if (view.mode != 0) Text(marksLine(view), style = MaterialTheme.typography.bodyMedium)
        HorizontalDivider()
        if (state.inner != state.dir) {
            Text("${state.share}:${state.inner}", style = MaterialTheme.typography.bodyMedium)
            FolderRow(stringResource(R.string.wizard_up)) { onOpen(state.copy(inner = parent(state.inner))) }
        }
        when {
            listing == null -> Text(stringResource(R.string.wizard_reading))
            listing.entries == null -> {
                Text(listing.error.orEmpty(), color = MaterialTheme.colorScheme.error)
                TextButton(onClick = onRetry) { Text(stringResource(R.string.wizard_retry)) }
            }
            listing.entries.isEmpty() -> Text(stringResource(R.string.wizard_empty))
            else -> for (e in listing.entries) {
                val at = child(state.inner, e.name)
                MarkRow(e, view.mode != 0, view.synced(at), { onToggle(at) }, if (e.dir) ({ onOpen(state.copy(inner = at)) }) else null)
            }
        }
        val ready = view.mode != 1 || view.marks.isNotEmpty()
        Buttons(onBack, stringResource(R.string.wizard_back), stringResource(R.string.wizard_next), enabled = ready, onNext)
    }
}

/** The marks as one line: "Synced only: Photos/, a.txt", or that there are none yet. */
@Composable
fun marksLine(view: MarksView): String = when {
    view.marks.isEmpty() && view.mode == 1 -> stringResource(R.string.wizard_marks_none_include)
    view.marks.isEmpty() -> stringResource(R.string.wizard_marks_none_exclude)
    view.mode == 1 -> stringResource(R.string.wizard_synced_only, view.marks.joinToString(", "))
    else -> stringResource(R.string.wizard_left_out, view.marks.joinToString(", "))
}

@Composable
private fun MarkRow(e: FolderEntry, marking: Boolean, synced: Int, onToggle: () -> Unit, onOpen: (() -> Unit)?) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        if (marking) {
            val box = when (synced) {
                2 -> ToggleableState.On
                1 -> ToggleableState.Indeterminate
                else -> ToggleableState.Off
            }
            TriStateCheckbox(box, onToggle)
        }
        // What is not synced is dimmed, as in the TUI.
        Text(
            if (e.dir) e.name + "/" else e.name,
            Modifier.weight(1f).clickable(enabled = onOpen != null) { onOpen?.invoke() }.padding(vertical = 12.dp),
            style = MaterialTheme.typography.bodyLarge,
            color = if (synced == 0) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
        )
    }
    HorizontalDivider()
}

/**
 * The wizard's fourth step: the folder on the phone, browsed from the top of its storage, or made
 * in the folder shown. The top itself is not chosen; on a phone with an SD card, the top of either
 * goes to the other.
 */
@Composable
fun WizardLocalStep(
    state: WizardState,
    volumes: List<Volume>,
    listing: Listing?,
    problem: String?,
    onOpen: (String) -> Unit,
    onMake: (String) -> String?,
    onRetry: () -> Unit,
    onBack: () -> Unit,
    onNext: () -> Unit,
) {
    var making by remember { mutableStateOf(false) }
    val top = storageTop(state.local, volumes)
    WizardPage(stringResource(R.string.wizard_local_title)) {
        Text(state.local, style = MaterialTheme.typography.bodyLarge)
        if (top) {
            for (v in volumes) if (v.path != state.local) FolderRow("${v.label} (${v.path})") { onOpen(v.path) }
        } else {
            FolderRow(stringResource(R.string.wizard_up)) { onOpen(parent(state.local)) }
        }
        when {
            listing == null -> Text(stringResource(R.string.wizard_reading))
            listing.entries == null -> {
                Text(listing.error.orEmpty(), color = MaterialTheme.colorScheme.error)
                TextButton(onClick = onRetry) { Text(stringResource(R.string.wizard_retry)) }
            }
            listing.entries.isEmpty() -> Text(stringResource(R.string.wizard_empty))
            else -> for (e in listing.entries) {
                if (e.dir) FolderRow(e.name + "/") { onOpen(child(state.local, e.name)) } else FolderRow(e.name, null)
            }
        }
        if (listing?.entries != null) TextButton(onClick = { making = true }) { Text(stringResource(R.string.wizard_new_folder)) }
        if (top) Text(stringResource(R.string.wizard_storage_top), style = MaterialTheme.typography.bodyMedium)
        Problem(problem)
        Buttons(onBack, stringResource(R.string.wizard_back), stringResource(R.string.wizard_use), enabled = listing?.entries != null && !top, onNext)
    }
    if (making) NewFolderDialog(state.local, onMake) { making = false }
}

/** Asks for the new folder's name; make makes it, or says why it could not. */
@Composable
private fun NewFolderDialog(dir: String, make: (String) -> String?, close: () -> Unit) {
    var name by remember { mutableStateOf("") }
    var refused by remember { mutableStateOf<String?>(null) }
    AlertDialog(
        onDismissRequest = close,
        title = { Text(stringResource(R.string.wizard_new_folder)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.wizard_new_folder_in, dir), style = MaterialTheme.typography.bodySmall)
                OutlinedTextField(name, { name = it }, label = { Text(stringResource(R.string.wizard_folder_name)) }, singleLine = true)
                refused?.let { Text(it, color = MaterialTheme.colorScheme.error) }
            }
        },
        confirmButton = {
            TextButton(onClick = {
                refused = make(name)
                if (refused == null) close()
            }) { Text(stringResource(R.string.wizard_make)) }
        },
        dismissButton = { TextButton(onClick = close) { Text(stringResource(R.string.add_cancel)) } },
    )
}

/**
 * The wizard's fifth step: the shuttle's profile, or none, each with its mode and triggers. What
 * the shuttle does not set itself comes from it.
 */
@Composable
fun WizardProfileStep(profiles: List<ProfileRow>, chosen: String, onChoose: (String) -> Unit, onBack: () -> Unit, onNext: () -> Unit) {
    WizardPage(stringResource(R.string.wizard_profile_title)) {
        Text(stringResource(R.string.wizard_profile_hint), style = MaterialTheme.typography.bodyMedium)
        val none = stringResource(R.string.wizard_no_profile)
        for ((name, line) in listOf("" to none) + profiles.map { it.name to listOf(it.mode.ifEmpty { "—" }, it.triggers).joinToString(" · ") }) {
            Row(
                Modifier.fillMaxWidth().selectable(selected = name == chosen) { onChoose(name) }.padding(vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                RadioButton(selected = name == chosen, onClick = { onChoose(name) })
                Column {
                    Text(name.ifEmpty { none }, style = MaterialTheme.typography.titleMedium)
                    if (name.isNotEmpty()) Text(line, style = MaterialTheme.typography.bodySmall)
                }
            }
        }
        Buttons(onBack, stringResource(R.string.wizard_back), stringResource(R.string.wizard_next), enabled = true, onNext)
    }
}

/** Above the wizard's last step, its settings: whence what it does not set comes, why it was not saved, and Save. */
@Composable
fun WizardSaveHead(profile: String, problem: String?, onSave: () -> Unit) {
    Text(
        if (profile.isEmpty()) stringResource(R.string.wizard_settings_default) else stringResource(R.string.wizard_settings_from, profile),
        style = MaterialTheme.typography.bodyMedium,
    )
    Problem(problem)
    Button(onClick = onSave, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.wizard_save)) }
}

@Composable
private fun Problem(problem: String?) {
    problem?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium) }
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

// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.json.JSONArray
import org.json.JSONObject

/** What a backup holds, as the binding's CheckBackup reads it, for the app to say before it restores it. */
data class BackupSummary(
    val shares: List<String>,
    val profiles: List<String>,
    val shuttles: List<String>,
    val noPassword: List<String>,
    val noFolder: List<String>,
) {
    companion object {
        fun parse(json: String): BackupSummary {
            val o = JSONObject(json)
            fun list(key: String) = (o.optJSONArray(key) ?: JSONArray()).let { a -> (0 until a.length()).map { a.getString(it) } }
            return BackupSummary(list("Shares"), list("Profiles"), list("Shuttles"), list("NoPassword"), list("NoFolder"))
        }
    }
}

/** What the Backup screen's buttons do. */
class BackupActions(val save: () -> Unit, val restore: () -> Unit, val confirm: () -> Unit, val cancel: () -> Unit)

/**
 * Backup and restore: the app's config file, its shares, profiles and shuttles, saved where the
 * user picks, and restored from one, asking first with what it holds. Passwords are not in it.
 * message is what the last save or restore came to; pending, a backup read and waiting to be
 * restored.
 */
@Composable
fun BackupScreen(message: String?, busy: Boolean, pending: BackupSummary?, actions: BackupActions, onBack: () -> Unit) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        TextButton(onClick = onBack) { Text(stringResource(R.string.back)) }
        Text(stringResource(R.string.backup_title), style = MaterialTheme.typography.headlineMedium)
        Text(stringResource(R.string.backup_what), style = MaterialTheme.typography.bodyMedium)
        Button(enabled = !busy, onClick = actions.save, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.backup_save)) }
        OutlinedButton(enabled = !busy, onClick = actions.restore, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.backup_restore)) }
        message?.let { Text(it, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.secondary) }
    }
    pending?.let { RestoreDialog(it, actions) }
}

@Composable
private fun RestoreDialog(s: BackupSummary, actions: BackupActions) {
    val none = stringResource(R.string.backup_none)
    fun names(l: List<String>) = l.joinToString(", ").ifEmpty { none }
    AlertDialog(
        onDismissRequest = actions.cancel,
        title = { Text(stringResource(R.string.backup_restore_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.backup_restore_what))
                Text(stringResource(R.string.backup_shares, names(s.shares)))
                Text(stringResource(R.string.backup_profiles, names(s.profiles)))
                Text(stringResource(R.string.backup_shuttles, names(s.shuttles)))
                if (s.noPassword.isNotEmpty()) {
                    Text(stringResource(R.string.backup_no_password, names(s.noPassword)), color = MaterialTheme.colorScheme.tertiary)
                }
                if (s.noFolder.isNotEmpty()) {
                    Text(stringResource(R.string.backup_no_folder, names(s.noFolder)), color = MaterialTheme.colorScheme.tertiary)
                }
            }
        },
        confirmButton = { TextButton(onClick = actions.confirm) { Text(stringResource(R.string.backup_restore_go)) } },
        dismissButton = { TextButton(onClick = actions.cancel) { Text(stringResource(R.string.add_cancel)) } },
    )
}

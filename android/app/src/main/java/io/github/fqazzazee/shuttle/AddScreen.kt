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
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import java.io.File

/**
 * The minimal way to add a first share and shuttle (step 16). Group C's editor, the TUI's wizard
 * in screens, replaces it.
 */
@Composable
fun AddScreen(onDone: () -> Unit) {
    var form by remember { mutableStateOf(AddForm()) }
    var result by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val failed = stringResource(R.string.add_no_daemon)
    val tested = stringResource(R.string.add_tested)

    fun background(f: () -> String?) {
        busy = true
        Thread {
            result = try {
                f()
            } catch (e: Exception) {
                e.message
            }
            busy = false
        }.start()
    }

    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(stringResource(R.string.add_title), style = MaterialTheme.typography.headlineMedium)

        Text(stringResource(R.string.add_share_heading), style = MaterialTheme.typography.titleMedium)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(form.smb, { form = form.copy(shareType = "smb") }, { Text("SMB") })
            FilterChip(!form.smb, { form = form.copy(shareType = "nfs3") }, { Text("NFS") })
        }
        Field(R.string.add_share_name, form.shareName) { form = form.copy(shareName = it) }
        Field(R.string.add_host, form.host) { form = form.copy(host = it) }
        Field(if (form.smb) R.string.add_share else R.string.add_export, form.share) { form = form.copy(share = it) }
        if (form.smb) {
            Field(R.string.add_user, form.user) { form = form.copy(user = it) }
            OutlinedTextField(
                form.password, { form = form.copy(password = it) }, Modifier.fillMaxWidth(),
                label = { Text(stringResource(R.string.add_password)) },
                visualTransformation = PasswordVisualTransformation(), singleLine = true,
            )
        }
        OutlinedButton(enabled = !busy, onClick = {
            background {
                val d = Core.current ?: return@background failed
                d.testShare(form.shareJSON(), form.password)
                tested
            }
        }) { Text(stringResource(R.string.add_test)) }

        Text(stringResource(R.string.add_shuttle_heading), style = MaterialTheme.typography.titleMedium)
        Field(R.string.add_name, form.name) { form = form.copy(name = it) }
        Field(R.string.add_folder, form.folder) { form = form.copy(folder = it) }
        Field(R.string.add_local, form.local) { form = form.copy(local = it) }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            for ((mode, label) in listOf("push" to R.string.mode_push, "pull" to R.string.mode_pull, "sync" to R.string.mode_sync)) {
                FilterChip(form.mode == mode, { form = form.copy(mode = mode) }, { Text(stringResource(label)) })
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            for ((runs, label) in listOf(Runs.ASKED to R.string.runs_asked, Runs.REAL_TIME to R.string.runs_realtime, Runs.HOURLY to R.string.runs_hourly)) {
                FilterChip(form.runs == runs, { form = form.copy(runs = runs) }, { Text(stringResource(label)) })
            }
        }

        val missing = form.missing().map { stringResource(it) }
        result?.let { Text(it, color = MaterialTheme.colorScheme.error.takeIf { _ -> it != tested } ?: MaterialTheme.colorScheme.primary) }
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            TextButton(onClick = onDone) { Text(stringResource(R.string.add_cancel)) }
            Button(enabled = !busy && missing.isEmpty(), onClick = {
                background {
                    val d = Core.current ?: return@background failed
                    // The folder the user named on the phone: made if it is not there yet.
                    File(form.local.trim()).mkdirs()
                    d.addShare(form.shareJSON(), form.password)
                    d.addShuttle(form.shuttleJSON())
                    onDone()
                    null
                }
            }) { Text(stringResource(R.string.add_save)) }
        }
        if (missing.isNotEmpty()) {
            Text(stringResource(R.string.add_missing, missing.joinToString(", ")), style = MaterialTheme.typography.bodySmall)
        }
    }
}

@Composable
private fun Field(label: Int, value: String, onChange: (String) -> Unit) {
    OutlinedTextField(value, onChange, Modifier.fillMaxWidth(), label = { Text(stringResource(label)) }, singleLine = true)
}

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
import androidx.compose.material3.AlertDialog
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
import org.json.JSONObject

/** A share as its editor has it: the config's fields (config.Share), and a password for the store. */
data class ShareForm(
    val type: String = "smb",
    val name: String = "",
    val host: String = "",
    val share: String = "",
    val user: String = "",
    val domain: String = "",
    val path: String = "",
    val password: String = "",
) {
    /** The share as SaveShare takes it, the port split off the host. */
    fun json(): String = JSONObject().apply {
        put("Name", name.trim())
        put("Type", type)
        when (type) {
            "path" -> put("Path", path.trim())
            else -> {
                val (h, port) = splitPort(host.trim())
                put("Host", h)
                if (port != null) put("Port", port)
                if (type == "smb") {
                    put("Share", share.trim())
                    put("User", user.trim())
                    if (domain.isNotBlank()) put("Domain", domain.trim())
                } else {
                    put("Export", share.trim())
                }
            }
        }
    }.toString()

    /** The fields still empty that must not be, by their labels. */
    fun missing(): List<Int> = buildList {
        if (name.isBlank()) add(R.string.add_share_name)
        when (type) {
            "path" -> if (!path.trim().startsWith("/")) add(R.string.share_path)
            else -> {
                if (host.isBlank()) add(R.string.add_host)
                if (share.isBlank()) add(if (type == "smb") R.string.add_share else R.string.add_export)
                if (type == "smb" && user.isBlank()) add(R.string.add_user)
            }
        }
    }

    companion object {
        /** A share as ShareJSON gives it, for its editor. */
        fun of(json: String): ShareForm {
            val o = JSONObject(json)
            val port = o.optInt("Port")
            val host = o.optString("Host")
            val type = o.getString("Type")
            return ShareForm(
                type = type,
                name = o.getString("Name"),
                host = if (port != 0) "$host:$port" else host,
                share = if (type == "nfs3") o.optString("Export") else o.optString("Share"),
                user = o.optString("User"),
                domain = o.optString("Domain"),
                path = o.optString("Path"),
            )
        }
    }
}

/** What the editor's buttons do; their outcome comes back as the editor's message. */
class ShareActions(
    val test: (ShareForm) -> Unit,
    val save: (ShareForm) -> Unit,
    val delete: (String) -> Unit,
    val cancel: () -> Unit,
)

/**
 * A share's editor, new or not (a share is not renamed): its kind and fields, a password for the
 * store, a test of the connection, and delete for one no shuttle uses. message is what the last
 * action came to: the test's result, or why a save or delete was refused.
 */
@Composable
fun ShareEditor(initial: ShareForm, isNew: Boolean, usedBy: List<String>, busy: Boolean, message: String?, actions: ShareActions) {
    var form by remember { mutableStateOf(initial) }
    var deleting by remember { mutableStateOf(false) }
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(
            if (isNew) stringResource(R.string.share_new) else stringResource(R.string.share_editing, form.name),
            style = MaterialTheme.typography.headlineMedium,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            for ((t, label) in listOf("smb" to "SMB", "nfs3" to "NFS", "path" to stringResource(R.string.share_folder))) {
                FilterChip(form.type == t, { form = form.copy(type = t) }, { Text(label) })
            }
        }
        Field(R.string.add_share_name, form.name, enabled = isNew) { form = form.copy(name = it) }
        if (form.type == "path") {
            Field(R.string.share_path, form.path) { form = form.copy(path = it) }
        } else {
            Field(R.string.add_host, form.host) { form = form.copy(host = it) }
            Field(if (form.type == "smb") R.string.add_share else R.string.add_export, form.share) { form = form.copy(share = it) }
        }
        if (form.type == "smb") {
            Field(R.string.add_user, form.user) { form = form.copy(user = it) }
            Field(R.string.share_domain, form.domain) { form = form.copy(domain = it) }
            OutlinedTextField(
                form.password, { form = form.copy(password = it) }, Modifier.fillMaxWidth(),
                label = { Text(stringResource(if (isNew) R.string.add_password else R.string.share_password_keep)) },
                visualTransformation = PasswordVisualTransformation(), singleLine = true,
            )
        }
        val missing = form.missing().map { stringResource(it) }
        OutlinedButton(enabled = !busy && missing.isEmpty(), onClick = { actions.test(form) }) { Text(stringResource(R.string.add_test)) }
        message?.let { Text(it, style = MaterialTheme.typography.bodyMedium) }
        if (missing.isNotEmpty()) {
            Text(stringResource(R.string.add_missing, missing.joinToString(", ")), style = MaterialTheme.typography.bodySmall)
        }
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            if (!isNew && usedBy.isEmpty()) TextButton(enabled = !busy, onClick = { deleting = true }) { Text(stringResource(R.string.share_delete)) }
            TextButton(onClick = actions.cancel) { Text(stringResource(R.string.add_cancel)) }
            Button(enabled = !busy && missing.isEmpty(), onClick = { actions.save(form) }) { Text(stringResource(R.string.add_save)) }
        }
        if (!isNew && usedBy.isNotEmpty()) {
            Text(stringResource(R.string.share_used_by, usedBy.joinToString(", ")), style = MaterialTheme.typography.bodySmall)
        }
    }
    if (deleting) {
        AlertDialog(
            onDismissRequest = { deleting = false },
            text = { Text(stringResource(R.string.share_delete_ask, form.name)) },
            confirmButton = { TextButton(onClick = { deleting = false; actions.delete(form.name) }) { Text(stringResource(R.string.share_delete)) } },
            dismissButton = { TextButton(onClick = { deleting = false }) { Text(stringResource(R.string.add_cancel)) } },
        )
    }
}

@Composable
private fun Field(label: Int, value: String, enabled: Boolean = true, onChange: (String) -> Unit) {
    OutlinedTextField(value, onChange, Modifier.fillMaxWidth(), enabled = enabled, label = { Text(stringResource(label)) }, singleLine = true)
}

/** A host and its port, when it ends in one: not an IPv6 address's last group. */
fun splitPort(host: String): Pair<String, Int?> {
    val i = host.lastIndexOf(':')
    val port = host.substring(i + 1).toIntOrNull()
    return if (i > 0 && port != null && host.indexOf(':') == i) host.substring(0, i) to port else host to null
}

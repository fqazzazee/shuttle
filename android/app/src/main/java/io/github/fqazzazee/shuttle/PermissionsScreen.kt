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
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp

/** The words for each permission: what it is called, and why shuttle needs it. */
private val Permission.title
    get() = when (this) {
        Permission.FILES -> R.string.perm_files
        Permission.LOCAL_NETWORK -> R.string.perm_network
        Permission.NOTIFICATIONS -> R.string.perm_notifications
    }

private val Permission.why
    get() = when (this) {
        Permission.FILES -> R.string.perm_files_why
        Permission.LOCAL_NETWORK -> R.string.perm_network_why
        Permission.NOTIFICATIONS -> R.string.perm_notifications_why
    }

/** First run: every permission the phone has, each explained, given or with a way to give it. */
@Composable
fun PermissionsScreen(access: Access, onAllow: (Permission) -> Unit, onContinue: () -> Unit) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(stringResource(R.string.perm_heading), style = MaterialTheme.typography.headlineMedium)
        Text(stringResource(R.string.perm_intro))
        for (p in Permission.entries) {
            PermissionCard(p, access.has(p), onAllow)
        }
        Button(onClick = onContinue, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.perm_continue))
        }
    }
}

@Composable
private fun PermissionCard(p: Permission, given: Boolean, onAllow: (Permission) -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(p.title), style = MaterialTheme.typography.titleMedium)
            Text(stringResource(p.why), style = MaterialTheme.typography.bodyMedium)
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End, verticalAlignment = Alignment.CenterVertically) {
                if (given) {
                    Text(stringResource(R.string.perm_given), color = MaterialTheme.colorScheme.primary)
                } else {
                    OutlinedButton(onClick = { onAllow(p) }) { Text(stringResource(R.string.perm_allow)) }
                }
            }
        }
    }
}

/** On the main screen: each permission still missing, with what it holds back, and a way to give it. */
@Composable
fun MissingPermissions(access: Access, onAllow: (Permission) -> Unit) {
    for (p in access.missing) {
        PermissionCard(p, false, onAllow)
    }
}

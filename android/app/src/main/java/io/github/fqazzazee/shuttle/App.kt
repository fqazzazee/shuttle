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
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp

/** What the App tab's rows open. */
class AppActions(val help: () -> Unit, val backup: () -> Unit, val about: () -> Unit)

/**
 * The App tab: the app's own settings and pages, apart from what it syncs: Help, Backup and
 * restore, About, and the wallpaper's colours. wallpaper is whether the app takes them in place of
 * shuttle's palette; null where Android has none to offer.
 */
@Composable
fun AppScreen(wallpaper: Boolean?, onWallpaper: (Boolean) -> Unit, actions: AppActions) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(stringResource(R.string.app_title), style = MaterialTheme.typography.headlineMedium)
        Page(R.string.help, R.string.app_help, actions.help)
        Page(R.string.backup_title, R.string.app_backup, actions.backup)
        Page(R.string.about, R.string.app_about, actions.about)
        wallpaper?.let { on ->
            Row(Modifier.fillMaxWidth().padding(vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.about_wallpaper), style = MaterialTheme.typography.titleMedium)
                    Text(stringResource(R.string.about_wallpaper_hint), style = MaterialTheme.typography.bodySmall)
                }
                Switch(on, onWallpaper)
            }
        }
    }
}

@Composable
private fun Page(title: Int, what: Int, onOpen: () -> Unit) {
    Column(Modifier.fillMaxWidth().clickable(onClick = onOpen).padding(vertical = 12.dp)) {
        Text(stringResource(title), style = MaterialTheme.typography.titleMedium)
        Text(stringResource(what), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
    HorizontalDivider()
}

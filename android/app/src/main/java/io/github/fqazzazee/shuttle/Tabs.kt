// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Menu
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource

/** The app's tabs: the TUI's, in its order, then the app's own. A notice opens one by its name (the Go core's notice.Note.Open). */
enum class Tab(val label: Int, val icon: ImageVector) {
    SHUTTLES(R.string.tab_shuttles, Icons.AutoMirrored.Filled.List),
    CONFLICTS(R.string.tab_conflicts, Icons.Filled.Warning),
    ACTIVITY(R.string.tab_activity, Icons.Filled.DateRange),
    PROFILES(R.string.tab_profiles, Icons.Filled.Settings),
    SHARES(R.string.tab_shares, Icons.Filled.Share),
    APP(R.string.tab_app, Icons.Filled.Menu),
    ;

    companion object {
        /** The tab by the name a notice gives it ("conflicts"), or null for none or one unknown. */
        fun of(name: String?): Tab? = entries.firstOrNull { it.name.equals(name, ignoreCase = true) }
    }
}

/** The tab bar at the bottom, and the selected tab's screen above it. */
@Composable
fun AppTabs(tab: Tab, onTab: (Tab) -> Unit, content: @Composable (Tab) -> Unit) {
    Scaffold(
        bottomBar = {
            NavigationBar {
                for (t in Tab.entries) {
                    NavigationBarItem(
                        selected = t == tab,
                        onClick = { onTab(t) },
                        icon = { Icon(t.icon, contentDescription = null) },
                        label = { Text(stringResource(t.label)) },
                    )
                }
            }
        },
    ) { padding ->
        // The bar's padding holds the system bars' insets: consumed here, or the screens pad twice.
        Box(Modifier.padding(padding).consumeWindowInsets(padding)) { content(tab) }
    }
}

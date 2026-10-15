// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.core.content.edit
import androidx.core.net.toUri

class MainActivity : ComponentActivity() {
    private var access by mutableStateOf(Access(files = false, localNetwork = false, notifications = false))
    private var firstRunSeen by mutableStateOf(false)
    private var forceStopped by mutableStateOf(false)
    private val pauses by lazy { Core.pauses(this) }
    private var pausedAll by mutableStateOf(false)
    private var adding by mutableStateOf(false)
    private var tab by mutableStateOf(Tab.SHUTTLES)
    private val prefs by lazy { getSharedPreferences("app", MODE_PRIVATE) }

    private val request = registerForActivityResult(ActivityResultContracts.RequestPermission()) {
        access = Access.of(this)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        openTab(intent)
        firstRunSeen = prefs.getBoolean(FIRST_RUN_SEEN, false)
        pausedAll = pauses.all
        // Opening the app after a force stop starts syncing again (the screen holds the daemon,
        // which starts the service and plans the jobs); say that it had stopped.
        val lastSeen = prefs.getLong(LAST_EXIT_SEEN, 0)
        val exits = recentExits(this)
        forceStopped = forceStoppedSince(exits, lastSeen)
        if (forceStopped) Log.i("shuttle", "force stop noticed; syncing starts again")
        prefs.edit { putLong(LAST_EXIT_SEEN, newestExit(exits, lastSeen)) }
        setContent {
            ShuttleTheme {
                Surface(Modifier.fillMaxSize()) {
                    if (!firstRunSeen) {
                        PermissionsScreen(access, ::allow) {
                            prefs.edit { putBoolean(FIRST_RUN_SEEN, true) }
                            firstRunSeen = true
                        }
                    } else if (adding) {
                        AddScreen { adding = false }
                    } else {
                        AppTabs(tab, { tab = it }) { t ->
                            when (t) {
                                Tab.SHUTTLES -> Main(
                                    access, Core.statuses, Core.startError, forceStopped, { forceStopped = false },
                                    ::allow, pausedAll, actions,
                                ) { adding = true }
                                else -> Later(t)
                            }
                        }
                    }
                }
            }
        }
    }

    // A notice's tap opens the app on its tab, whether the app was open or not.
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        openTab(intent)
    }

    private fun openTab(intent: Intent?) {
        Tab.of(intent?.getStringExtra(Notices.EXTRA_TAB))?.let { tab = it }
    }

    // The screen holds the daemon while it is shown, and starts the foreground service if a
    // shuttle is real-time.
    override fun onStart() {
        super.onStart()
        Core.async { Core.acquire(this, SCREEN)?.let { Core.updateService(this, it) } }
    }

    override fun onStop() {
        Core.async { Core.release(SCREEN) }
        super.onStop()
    }

    // Permissions are given in Settings and in dialogs, and can be taken back there at any
    // time: look again whenever the app comes back, and tell the daemon.
    override fun onResume() {
        super.onResume()
        access = Access.of(this)
        val now = access
        Core.async { Core.setAccess(now) }
    }

    private val actions = ListActions(
        syncNow = { SyncService.syncNow(this, it) },
        setPaused = { name, paused ->
            pauses.set(name, paused)
            Core.async { Core.current?.setPaused(name, paused) }
        },
        setPausedAll = { paused ->
            pauses.setAll(paused)
            pausedAll = paused
            Core.async { Core.current?.setPaused("", paused) }
        },
    )

    private fun allow(p: Permission) {
        val name = p.system
        val asked = "asked:" + p.name
        val how = ask(p, prefs.getBoolean(asked, false), name != null && shouldShowRequestPermissionRationale(name))
        val app = "package:$packageName".toUri()
        when (how) {
            Ask.ALL_FILES_SETTINGS -> startActivity(Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION, app))
            Ask.APP_SETTINGS -> startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, app))
            Ask.DIALOG -> {
                prefs.edit { putBoolean(asked, true) }
                name?.let { request.launch(it) }
            }
        }
    }

    private companion object {
        const val FIRST_RUN_SEEN = "first_run_seen"
        const val SCREEN = "screen"
        const val LAST_EXIT_SEEN = "last_exit_seen"
    }
}

/** The app's main screen: what needs saying first, then the shuttles. */
@Composable
private fun Main(
    access: Access,
    statuses: Map<String, String>,
    startError: String?,
    forceStopped: Boolean,
    onSeen: () -> Unit,
    onAllow: (Permission) -> Unit,
    pausedAll: Boolean,
    actions: ListActions,
    onAdd: () -> Unit,
) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.app_name), style = MaterialTheme.typography.headlineMedium)
        if (forceStopped && statuses.isNotEmpty()) ForceStopped(onSeen)
        MissingPermissions(access, onAllow)
        startError?.let { Text(stringResource(R.string.start_failed, it), color = MaterialTheme.colorScheme.error) }
        if (statuses.isEmpty()) {
            Text(stringResource(R.string.not_set_up))
        } else {
            ShuttleList(statuses.toSortedMap().values.map(ShuttleStatus::parse), pausedAll, actions)
        }
        OutlinedButton(onClick = onAdd, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.add)) }
    }
}

/** A tab whose screen comes in a later step of the app's phase. */
@Composable
private fun Later(tab: Tab) {
    Column(Modifier.safeDrawingPadding().padding(24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(stringResource(tab.label), style = MaterialTheme.typography.headlineMedium)
        Text(stringResource(R.string.tab_later))
    }
}

/** After a force stop: nothing synced while the app was stopped, and it syncs again now. */
@Composable
private fun ForceStopped(onSeen: () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.force_stopped_title), style = MaterialTheme.typography.titleMedium)
            Text(stringResource(R.string.force_stopped_text))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = onSeen) { Text(stringResource(R.string.ok)) }
            }
        }
    }
}

/** Light or dark as the phone is set; in the phone's own colours from Android 12, which has them. */
@Composable
fun ShuttleTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val colors = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
        val context = LocalContext.current
        if (dark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
    } else {
        if (dark) darkColorScheme() else lightColorScheme()
    }
    MaterialTheme(colorScheme = colors, content = content)
}

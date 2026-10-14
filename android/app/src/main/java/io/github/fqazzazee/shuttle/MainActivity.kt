// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
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
import org.json.JSONObject

class MainActivity : ComponentActivity() {
    private var access by mutableStateOf(Access(files = false, localNetwork = false, notifications = false))
    private var firstRunSeen by mutableStateOf(false)
    private val prefs by lazy { getSharedPreferences("app", MODE_PRIVATE) }

    private val request = registerForActivityResult(ActivityResultContracts.RequestPermission()) {
        access = Access.of(this)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        firstRunSeen = prefs.getBoolean(FIRST_RUN_SEEN, false)
        setContent {
            ShuttleTheme {
                Surface(Modifier.fillMaxSize()) {
                    if (!firstRunSeen) {
                        PermissionsScreen(access, ::allow) {
                            prefs.edit { putBoolean(FIRST_RUN_SEEN, true) }
                            firstRunSeen = true
                        }
                    } else {
                        Main(access, Core.statuses, Core.startError, ::allow)
                    }
                }
            }
        }
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
    }
}

/** Until the shuttle list (step 14): each shuttle's name and state, and why it waits. */
@Composable
private fun Main(access: Access, statuses: Map<String, String>, startError: String?, onAllow: (Permission) -> Unit) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.app_name), style = MaterialTheme.typography.headlineMedium)
        MissingPermissions(access, onAllow)
        startError?.let { Text(stringResource(R.string.start_failed, it), color = MaterialTheme.colorScheme.error) }
        if (statuses.isEmpty()) Text(stringResource(R.string.not_set_up))
        for ((name, json) in statuses.toSortedMap()) {
            val st = JSONObject(json)
            val waiting = st.optString("Waiting")
            Text(if (waiting.isEmpty()) "$name: ${st.getString("State")}" else "$name: $waiting")
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

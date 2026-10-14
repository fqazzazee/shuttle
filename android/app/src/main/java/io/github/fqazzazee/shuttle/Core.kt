// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.Context
import android.content.Intent
import android.os.Build
import android.provider.Settings
import android.util.Log
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import io.github.fqazzazee.shuttle.mobile.Daemon
import io.github.fqazzazee.shuttle.mobile.Listener
import io.github.fqazzazee.shuttle.mobile.Mobile
import io.github.fqazzazee.shuttle.mobile.Options
import java.util.concurrent.Executors
import org.json.JSONArray
import org.json.JSONObject

/**
 * The process's one daemon (DECISIONS 2026-10-14, "Android: how shuttles run"), and what it says.
 * Starting it opens every state database and stopping it lets a run finish its file, so both
 * happen on Core's own thread, one after the other in the order asked: never on the main thread,
 * and a screen that closes and opens again cannot overtake itself.
 */
object Core {
    private const val TAG = "shuttle"
    private val thread = Executors.newSingleThreadExecutor { Thread(it, "shuttle-core") }
    private val daemon = Holders<Daemon>(stop = { it.stop() })

    /** Each shuttle's status as the daemon last sent it, as JSON, by name. */
    var statuses by mutableStateOf(emptyMap<String, String>())
        private set

    /** Why the daemon could not start, or null. */
    var startError by mutableStateOf<String?>(null)
        private set

    /** Runs f on Core's thread, after everything asked of it before. */
    fun async(f: () -> Unit) {
        thread.execute {
            try {
                f()
            } catch (e: Exception) {
                Log.e(TAG, "core", e)
            }
        }
    }

    /** Holds the daemon for holder, starting it if no one held it. On Core's thread only. */
    fun acquire(context: Context, holder: String): Daemon? {
        val app = context.applicationContext
        return try {
            daemon.acquire(holder) { start(app) }.also { startError = null }
        } catch (e: Exception) {
            Log.e(TAG, "the daemon did not start", e)
            startError = e.message
            null
        }
    }

    /** Lets go of the daemon for holder; the last one out stops it. On Core's thread only. */
    fun release(holder: String) = daemon.release(holder)

    /** The running daemon, if someone holds it. */
    val current: Daemon? get() = daemon.current

    /** Tells the daemon what the phone has given, as the app found it on coming back. */
    fun setAccess(access: Access) {
        current?.setPermissions(access.files, access.localNetwork)
    }

    /**
     * Runs the foreground service while a shuttle is real-time, and only then: a shuttle that is
     * only scheduled shows no permanent notification. On Core's thread, with the daemon held.
     */
    fun updateService(context: Context, d: Daemon) {
        val intent = Intent(context, SyncService::class.java)
        if (d.realTime()) context.startForegroundService(intent) else context.stopService(intent)
    }

    private fun start(app: Context): Daemon {
        val files = AppFiles(app.filesDir, app.noBackupFilesDir)
        val access = Access.of(app)
        val o = Options().apply {
            configPath = files.config.path
            stateDir = files.state.path
            deviceName = deviceName(app)
            passwords = PasswordStore(files.passwords, KeystoreKey::get)
            listener = object : Listener {
                // From the daemon's runners, several at once.
                override fun onStatus(statusJSON: String) {
                    val name = JSONObject(statusJSON).getString("Name")
                    synchronized(this@Core) { statuses = statuses + (name to statusJSON) }
                }

                // The notification channels come with step 15; until then the events are logged.
                override fun onEvent(eventJSON: String) {
                    Log.i(TAG, "event $eventJSON")
                }
            }
            filesAccess = access.files
            localNetwork = access.localNetwork
        }
        return Mobile.start(o).also { d ->
            val all = JSONArray(d.statusesJSON())
            synchronized(this) {
                statuses = (0 until all.length()).associate { i ->
                    all.getJSONObject(i).let { it.getString("Name") to it.toString() }
                }
            }
        }
    }

    /** The phone's name as its owner set it, for conflict copies; its model when there is none. */
    private fun deviceName(context: Context): String =
        Settings.Global.getString(context.contentResolver, Settings.Global.DEVICE_NAME)?.takeIf { it.isNotBlank() }
            ?: Build.MODEL
}

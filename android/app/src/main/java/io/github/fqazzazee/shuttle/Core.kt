// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
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
import java.time.Instant
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger
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
    private val daemon = Holders<Daemon>(stop = { unwatchNetwork(); it.stop() })
    private var network: Pair<ConnectivityManager, ConnectivityManager.NetworkCallback>? = null

    /** Each shuttle's status as the daemon last sent it, as JSON, by name. */
    var statuses by mutableStateOf(emptyMap<String, String>())
        private set

    /** Runs the user asked for that the service is seeing through (Sync now): it stays while there are any. */
    val manualRuns = AtomicInteger()

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

    /** Runs f on Core's thread, after everything asked of it before, and waits for it. Never on Core's thread. */
    fun <T> call(f: () -> T): T = thread.submit(f).get()

    /** Holds the daemon for holder, starting it if no one held it, and plans the jobs. On Core's thread only. */
    fun acquire(context: Context, holder: String): Daemon? {
        val app = context.applicationContext
        return try {
            daemon.acquire(holder) { start(app) }.also {
                startError = null
                plan(app, it)
            }
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

    /** Plans WorkManager's jobs for the shuttles that are not real-time. On Core's thread, with the daemon held. */
    fun plan(context: Context, d: Daemon) {
        try {
            ShuttleWorker.schedule(context, planWork(parseWork(d.workJSON()), Instant.now()))
        } catch (e: Exception) {
            Log.e(TAG, "planning the jobs", e)
        }
    }

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
        when {
            d.realTime() -> context.startForegroundService(intent)
            manualRuns.get() == 0 -> context.stopService(intent)
        }
    }

    /** The user's pauses, kept by the app for every daemon it starts. */
    fun pauses(context: Context) = Pauses(context.getSharedPreferences("app", Context.MODE_PRIVATE))

    private fun start(app: Context): Daemon {
        Notices.channels(app)
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

                override fun onNotice(noteJSON: String) {
                    Notices.show(app, Notice.parse(noteJSON))
                }

                // The config changed: drop the shuttles it removed, and see again whether the
                // service is needed and what work to plan.
                override fun onReload(reloadedJSON: String) {
                    val removed = JSONObject(reloadedJSON).optJSONArray("Removed")
                    if (removed != null) {
                        val names = (0 until removed.length()).map { removed.getString(it) }.toSet()
                        synchronized(this@Core) { statuses = statuses - names }
                    }
                    async {
                        current?.let {
                            updateService(app, it)
                            plan(app, it)
                        }
                    }
                }
            }
            filesAccess = access.files
            localNetwork = access.localNetwork
            val pauses = pauses(app)
            pausedJSON = pauses.json()
            pausedAll = pauses.all
        }
        return Mobile.start(o).also { d ->
            watchNetwork(app, d)
            val all = JSONArray(d.statusesJSON())
            synchronized(this) {
                statuses = (0 until all.length()).associate { i ->
                    all.getJSONObject(i).let { it.getString("Name") to it.toString() }
                }
            }
        }
    }

    /**
     * While the daemon runs: whether the connection is metered, from Android's own view of it
     * (a shuttle set to pause on one does), and a look for the shares whenever the phone joins a
     * network, rather than at the next probe.
     */
    private fun watchNetwork(app: Context, d: Daemon) {
        val cm = app.getSystemService(ConnectivityManager::class.java)
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) = d.probeNow()

            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                d.setMetered(!caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED))
            }
        }
        cm.registerDefaultNetworkCallback(callback)
        network = cm to callback
    }

    private fun unwatchNetwork() {
        network?.let { (cm, callback) -> cm.unregisterNetworkCallback(callback) }
        network = null
    }

    /** The phone's name as its owner set it, for conflict copies; its model when there is none. */
    private fun deviceName(context: Context): String =
        Settings.Global.getString(context.contentResolver, Settings.Global.DEVICE_NAME)?.takeIf { it.isNotBlank() }
            ?: Build.MODEL
}

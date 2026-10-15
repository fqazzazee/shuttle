// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.app.Notification
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import java.util.concurrent.TimeUnit

/**
 * Holds the daemon while a shuttle is real-time, with the permanent notification Android asks of
 * it: the process then lives through the screen going off, Doze and a swipe from recents, and
 * the daemon's watcher and poll run as on the desktop. Type specialUse, not dataSync: dataSync may
 * run 6 hours in 24, and a watcher runs all day.
 */
class SyncService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val sync = intent?.getStringExtra(EXTRA_SYNC)
        // specialUse came with Android 14; before it, a foreground service has no type to give.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(NOTIFICATION, notification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIFICATION, notification())
        }
        if (sync != null) Core.manualRuns.incrementAndGet()
        Core.async {
            val d = Core.acquire(this, HOLDER)
            when {
                d == null -> {
                    if (sync != null) Core.manualRuns.decrementAndGet()
                    stopSelf()
                }
                // A run the user asked for (DECISIONS 2026-10-14: manual runs run in the
                // service for their duration): leaving the app does not cut it short.
                sync != null -> {
                    d.syncNow(sync)
                    Thread {
                        try {
                            d.waitQuiet(TimeUnit.MINUTES.toMillis(60))
                        } catch (e: Exception) {
                            android.util.Log.w("shuttle", "sync now", e)
                        }
                        Core.manualRuns.decrementAndGet()
                        Core.async { if (!d.realTime() && Core.manualRuns.get() == 0) stopSelf() }
                    }.start()
                }
                // No real-time shuttle any more, and no run to see through: nothing to keep running.
                !d.realTime() && Core.manualRuns.get() == 0 -> stopSelf()
            }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        Core.async { Core.release(HOLDER) }
        super.onDestroy()
    }

    private fun notification(): Notification {
        Notices.channels(this)
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
        )
        return Notification.Builder(this, Channel.SERVICE.id)
            .setSmallIcon(R.drawable.ic_launcher_monochrome)
            .setContentTitle(getString(R.string.service_title))
            .setContentText(getString(R.string.service_text))
            .setContentIntent(open)
            .setOngoing(true)
            .build()
    }

    companion object {
        /** Runs the named shuttle now, in the service, for as long as the run takes. */
        fun syncNow(context: android.content.Context, name: String) {
            context.startForegroundService(Intent(context, SyncService::class.java).putExtra(EXTRA_SYNC, name))
        }

        private const val EXTRA_SYNC = "sync"
        private const val HOLDER = "service"
        private const val NOTIFICATION = 1
    }
}

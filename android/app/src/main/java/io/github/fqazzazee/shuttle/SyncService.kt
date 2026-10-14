// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder

/**
 * Holds the daemon while a shuttle is real-time, with the permanent notification Android asks of
 * it: the process then lives through the screen going off, Doze and a swipe from recents, and
 * the daemon's watcher and poll run as on the desktop. Type specialUse, not dataSync: dataSync may
 * run 6 hours in 24, and a watcher runs all day.
 */
class SyncService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // specialUse came with Android 14; before it, a foreground service has no type to give.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(NOTIFICATION, notification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIFICATION, notification())
        }
        Core.async {
            val d = Core.acquire(this, HOLDER)
            // No real-time shuttle any more (or none could start): nothing to keep running.
            if (d == null || !d.realTime()) stopSelf()
        }
        return START_STICKY
    }

    override fun onDestroy() {
        Core.async { Core.release(HOLDER) }
        super.onDestroy()
    }

    private fun notification(): Notification {
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL, getString(R.string.service_channel), NotificationManager.IMPORTANCE_LOW),
        )
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
        )
        return Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_launcher_foreground)
            .setContentTitle(getString(R.string.service_title))
            .setContentText(getString(R.string.service_text))
            .setContentIntent(open)
            .setOngoing(true)
            .build()
    }

    private companion object {
        const val HOLDER = "service"
        const val CHANNEL = "service"
        const val NOTIFICATION = 1
    }
}

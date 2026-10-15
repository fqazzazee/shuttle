// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import java.util.concurrent.atomic.AtomicInteger
import org.json.JSONObject

/** What one notification says, as the desktop words it (the Go core's notice.Note), and the tab it opens. */
data class Notice(val key: String, val kind: String, val summary: String, val body: String, val open: String = "") {
    companion object {
        fun parse(json: String): Notice {
            val o = JSONObject(json)
            return Notice(o.optString("Key"), o.getString("Kind"), o.getString("Summary"), o.optString("Body"), o.optString("Open"))
        }
    }
}

/**
 * The notification channels (DECISIONS 2026-10-14, "Android: notifications"): one per kind of
 * notice, as the notify switches name them, so that each can also be muted in the phone's
 * settings, and the foreground service's own.
 */
enum class Channel(val id: String, val title: Int, val importance: Int) {
    ERRORS("errors", R.string.channel_errors, NotificationManager.IMPORTANCE_DEFAULT),
    CONFLICTS("conflicts", R.string.channel_conflicts, NotificationManager.IMPORTANCE_DEFAULT),
    COMPLETED("completed", R.string.channel_completed, NotificationManager.IMPORTANCE_LOW),
    SHARE("share", R.string.channel_share, NotificationManager.IMPORTANCE_LOW),
    SERVICE("service", R.string.service_channel, NotificationManager.IMPORTANCE_LOW),
    ;

    companion object {
        /** The channel of a notice's kind; one the app does not know of is an error, to be seen. */
        fun of(kind: String) = entries.firstOrNull { it.id == kind && it != SERVICE } ?: ERRORS
    }
}

/**
 * Where a notice goes in the notification shade: one with a key replaces the last with the same
 * key (a shuttle's latest error, a share's latest state); one without (a batch of conflicts) is
 * news of its own.
 */
fun tagOf(n: Notice, seq: Int) = n.key.ifEmpty { "${n.kind}#$seq" }

object Notices {
    private val seq = AtomicInteger()

    /** The tab the app opens on, from a notice's tap. */
    const val EXTRA_TAB = "tab"

    /** Makes the channels; Android keeps them, and leaves the user's settings for each alone. */
    fun channels(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java)
        for (c in Channel.entries) {
            manager.createNotificationChannel(NotificationChannel(c.id, context.getString(c.title), c.importance))
        }
    }

    /** Shows a notice in its channel. Without the notifications permission, Android drops it. */
    fun show(context: Context, n: Notice) {
        // One pending intent per tab: Android would otherwise reuse the first, extras and all.
        val tab = Tab.of(n.open)
        val open = PendingIntent.getActivity(
            context, tab?.ordinal ?: -1,
            Intent(context, MainActivity::class.java).putExtra(EXTRA_TAB, tab?.name),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        val note = Notification.Builder(context, Channel.of(n.kind).id)
            .setSmallIcon(R.drawable.ic_launcher_foreground)
            .setContentTitle(n.summary)
            .setContentText(n.body.lineSequence().first())
            .setStyle(Notification.BigTextStyle().bigText(n.body))
            .setContentIntent(open)
            .setAutoCancel(true)
            .build()
        context.getSystemService(NotificationManager::class.java).notify(tagOf(n, seq.incrementAndGet()), 0, note)
    }
}

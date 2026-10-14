// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/**
 * After the phone starts, and after the app is updated, starts the foreground service again if a
 * shuttle is real-time: without it, real-time syncing would wait for the app to be opened.
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED && intent.action != Intent.ACTION_MY_PACKAGE_REPLACED) return
        val pending = goAsync()
        Core.async {
            try {
                Core.acquire(context, HOLDER)?.let { Core.updateService(context, it) }
                Core.release(HOLDER)
            } finally {
                pending.finish()
            }
        }
    }

    private companion object {
        const val HOLDER = "boot"
    }
}

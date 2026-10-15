// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.app.Application
import io.github.fqazzazee.shuttle.mobile.Mobile

/**
 * The app's process. It starts the Go core's library on the main thread before anything else in
 * the process can touch it: gomobile's classes start it from their static blocks, and two of them
 * started at once on two threads wait on each other for good. That happened when the app was opened
 * while it was being updated: MainActivity began Mobile's, BootReceiver's thread Options', and the
 * app froze ("isn't responding").
 */
class ShuttleApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        Mobile.touch()
    }
}

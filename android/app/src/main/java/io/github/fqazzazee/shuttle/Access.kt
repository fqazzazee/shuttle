// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Environment

/** The permissions the app asks for up front (DECISIONS 2026-10-14, "Android: all-files access and local network"). */
enum class Permission {
    /** "All files access": the folders being synced, in the phone's shared storage. */
    FILES,

    /** Android 17's local network access: without it, connections to the NAS time out. */
    LOCAL_NETWORK,

    /** Errors, conflicts, completed runs, and the service's own notification. */
    NOTIFICATIONS,
}

/**
 * The system's name for p, asked for in a dialog; null for all files access, which has its own
 * settings page, and for a permission the phone's Android does not have.
 */
val Permission.system: String?
    get() = when (this) {
        Permission.FILES -> null
        Permission.LOCAL_NETWORK ->
            if (Build.VERSION.SDK_INT >= Access.ANDROID_17) Manifest.permission.ACCESS_LOCAL_NETWORK else null
        Permission.NOTIFICATIONS ->
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) Manifest.permission.POST_NOTIFICATIONS else null
    }

/** Which permissions the user has given. One that the phone's Android does not have counts as given. */
data class Access(val files: Boolean, val localNetwork: Boolean, val notifications: Boolean) {
    fun has(p: Permission) = when (p) {
        Permission.FILES -> files
        Permission.LOCAL_NETWORK -> localNetwork
        Permission.NOTIFICATIONS -> notifications
    }

    /** The permissions still to ask for, in the order the first-run screen shows them. */
    val missing get() = Permission.entries.filter { !has(it) }

    companion object {
        /** Android 17, where local network access became a permission. */
        const val ANDROID_17 = 37

        /** What the phone says now. Asked again whenever the app comes back to the foreground. */
        fun of(context: Context): Access {
            fun granted(p: Permission) =
                p.system?.let { context.checkSelfPermission(it) == PackageManager.PERMISSION_GRANTED } ?: true
            return Access(
                files = Environment.isExternalStorageManager(),
                localNetwork = granted(Permission.LOCAL_NETWORK),
                notifications = granted(Permission.NOTIFICATIONS),
            )
        }
    }
}

/** How the app asks for a permission. */
enum class Ask {
    /** All files access is given only on its own settings page. */
    ALL_FILES_SETTINGS,

    /** The system's dialog. */
    DIALOG,

    /** The app's settings page: Android no longer shows the dialog for it. */
    APP_SETTINGS,
}

/**
 * How to ask for p. Android shows no rationale both before the first request and once the user
 * has said no for good, so the app keeps whether it has asked before: asked, and no rationale,
 * means the dialog would not show.
 */
fun ask(p: Permission, askedBefore: Boolean, rationale: Boolean): Ask = when {
    p == Permission.FILES -> Ask.ALL_FILES_SETTINGS
    askedBefore && !rationale -> Ask.APP_SETTINGS
    else -> Ask.DIALOG
}

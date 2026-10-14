// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.app.ActivityManager
import android.app.ApplicationExitInfo
import android.content.Context

/** One way the app's process ended, as Android keeps it: when, and why (ApplicationExitInfo.REASON_*). */
data class Exit(val time: Long, val reason: Int)

/**
 * Whether the app was force-stopped since lastSeen (DECISIONS 2026-10-14, "Android: how shuttles
 * run": a force stop leaves the app stopped, with no service and no jobs, until it is opened;
 * the app says so when opened). Android counts a force stop from the app's settings as the user's
 * request.
 */
fun forceStoppedSince(exits: List<Exit>, lastSeen: Long): Boolean =
    exits.any { it.time > lastSeen && it.reason == ApplicationExitInfo.REASON_USER_REQUESTED }

/** The newest exit's time, or lastSeen if there is none newer: what the next check counts from. */
fun newestExit(exits: List<Exit>, lastSeen: Long): Long = maxOf(lastSeen, exits.maxOfOrNull { it.time } ?: 0)

/** The process's recent exits, newest first, as Android keeps them. */
fun recentExits(context: Context): List<Exit> =
    context.getSystemService(ActivityManager::class.java)
        .getHistoricalProcessExitReasons(context.packageName, 0, 16)
        .map { Exit(it.timestamp, it.reason) }

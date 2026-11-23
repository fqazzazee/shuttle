// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.util.concurrent.atomic.AtomicInteger
import org.json.JSONArray

/**
 * Waits until the daemon is quiet, for as long as a run is going (DECISIONS 2026-10-14: manual
 * runs run in the service for their duration). Only time with nothing running counts against
 * idleSteps: a daemon that never turns quiet with no run going does not keep the service forever.
 */
fun waitWhileBusy(
    waitQuiet: (Long) -> Unit,
    running: () -> Boolean,
    destroyed: () -> Boolean,
    stepMillis: Long = 60_000,
    idleSteps: Int = 60,
) {
    var idle = 0
    while (!destroyed()) {
        try {
            waitQuiet(stepMillis)
            return
        } catch (_: Exception) {
            // The binding's only error here is that the step ran out.
        }
        idle = if (running()) 0 else idle + 1
        if (idle >= idleSteps) return
    }
}

/**
 * Asks for a run and sees it through in the background. However it ends, it stops counting in
 * runs and ended is called: a run still counted keeps the foreground service up.
 */
fun seeThrough(
    runs: AtomicInteger,
    sync: () -> Unit,
    wait: () -> Unit,
    failed: (Exception) -> Unit,
    ended: () -> Unit,
    background: (() -> Unit) -> Unit = { Thread(it).start() },
) {
    try {
        sync()
    } catch (e: Exception) {
        runs.decrementAndGet()
        try {
            failed(e)
        } finally {
            ended()
        }
        return
    }
    background {
        try {
            wait()
        } finally {
            runs.decrementAndGet()
            ended()
        }
    }
}

/** Whether the binding's StatusesJSON has a shuttle in a run. */
fun anyRunning(statusesJSON: String): Boolean {
    val all = JSONArray(statusesJSON)
    return (0 until all.length()).any { all.getJSONObject(it).optString("State") == "running" }
}

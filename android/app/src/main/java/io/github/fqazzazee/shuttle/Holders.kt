// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

/**
 * One thing shared by whoever holds it: started for the first holder, stopped when the last one
 * lets go. The process's daemon is one (DECISIONS 2026-10-14, "Android: how shuttles run"): the
 * screen holds it while it is shown, the foreground service while it runs, a worker for its run.
 */
class Holders<T : Any>(private val stop: (T) -> Unit) {
    private val holders = mutableSetOf<String>()

    // Volatile so that current takes no lock: starting and stopping hold it for as long as they
    // take, a stop for the rest of a file, and the main thread reads current.
    @Volatile private var thing: T? = null

    /** Starts it if no one held it, and returns it. A holder that already holds it is counted once. */
    @Synchronized
    fun acquire(holder: String, start: () -> T): T {
        val t = thing ?: start().also { thing = it }
        holders += holder
        return t
    }

    /** Lets go for holder; the last one out stops it. */
    @Synchronized
    fun release(holder: String) {
        if (!holders.remove(holder) || holders.isNotEmpty()) return
        val stopping = thing
        thing = null
        stopping?.let(stop)
    }

    /** It, while someone holds it; null once it is stopping. Never waits. */
    val current: T? get() = thing
}

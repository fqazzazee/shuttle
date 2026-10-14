// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.time.Duration
import java.time.Instant
import java.time.OffsetDateTime
import org.json.JSONArray

/** What the daemon asks WorkManager to run for one shuttle that is not real-time (the binding's WorkJSON). */
data class Work(
    val name: String,
    val nextRun: Instant?,
    val shareAvailable: Boolean,
    val network: Boolean,
    val unmetered: Boolean,
)

/** The network a job waits for. */
enum class Needs { NOTHING, NETWORK, UNMETERED }

/**
 * One job for WorkManager, by a name unique to it. A schedule's job is named for its time as well:
 * a later plan keeps it if the time is the same and cancels it if not, but never one that is
 * running, which would stop the worker that is making the plan.
 */
data class Job(val name: String, val needs: Needs, val delay: Duration?, val every: Duration?)

/** The prefix of every job's unique name: what a plan replaces or cancels. */
const val JOB_PREFIX = "shuttle:"

/** WorkManager's shortest period. */
val SHARE_CHECK_EVERY: Duration = Duration.ofMinutes(15)

fun parseWork(json: String): List<Work> {
    val all = JSONArray(json)
    return (0 until all.length()).map { i ->
        val w = all.getJSONObject(i)
        Work(
            name = w.getString("Name"),
            nextRun = w.optString("NextRun").takeIf { it.isNotEmpty() }?.let { OffsetDateTime.parse(it).toInstant() },
            shareAvailable = w.getBoolean("ShareAvailable"),
            network = w.getBoolean("Network"),
            unmetered = w.getBoolean("Unmetered"),
        )
    }
}

/**
 * The jobs for the work, at now (DECISIONS 2026-10-14, "Android: how shuttles run"):
 * - a schedule: one job at its next run, made again after each worker. Approximate: Android
 *   runs it then or later, while the phone is idle.
 * - share-available: a job every 15 minutes while the phone is on a network. Its worker's daemon
 *   probes the share at start, and the shuttle runs if it is there.
 * A shuttle whose runs wait for a connection that is not metered waits for one here too.
 */
fun planWork(work: List<Work>, now: Instant): List<Job> = work.flatMap { w ->
    val needs = when {
        w.unmetered && w.network -> Needs.UNMETERED
        w.network -> Needs.NETWORK
        else -> Needs.NOTHING
    }
    buildList {
        w.nextRun?.let {
            val delay = Duration.between(now, it).coerceAtLeast(Duration.ZERO)
            add(Job("${JOB_PREFIX}schedule:${w.name}@${it.epochSecond}", needs, delay, null))
        }
        if (w.shareAvailable) add(Job("${JOB_PREFIX}share:${w.name}", needs, null, SHARE_CHECK_EVERY))
    }
}

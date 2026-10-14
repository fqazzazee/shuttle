// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.Context
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.Worker
import androidx.work.WorkerParameters
import java.util.concurrent.TimeUnit

/**
 * Runs what is due for the shuttles that are not real-time: holds the daemon (the running one, if
 * the screen or the service has it), which runs what is due by itself, waits until all is quiet,
 * plans the next jobs and lets go.
 */
class ShuttleWorker(context: Context, params: WorkerParameters) : Worker(context, params) {
    override fun doWork(): Result {
        val holder = "worker:$id"
        val d = Core.call { Core.acquire(applicationContext, holder) } ?: return Result.success()
        try {
            // WorkManager stops a worker after ten minutes; a longer run carries on in the next one.
            d.waitQuiet(TimeUnit.MINUTES.toMillis(9))
        } catch (e: Exception) {
            android.util.Log.w("shuttle", "worker", e)
        } finally {
            // The runs moved the schedules on: plan the next jobs before letting go.
            Core.call {
                Core.plan(applicationContext, d)
                Core.release(holder)
            }
        }
        return Result.success()
    }

    companion object {
        /**
         * Puts the jobs in WorkManager: a schedule's job is kept if it is already there, a
         * share check's updated, and jobs no longer in the plan are cancelled unless running.
         */
        fun schedule(context: Context, jobs: List<Job>) {
            val wm = WorkManager.getInstance(context)
            for (job in jobs) {
                val constraints = Constraints.Builder().setRequiredNetworkType(
                    when (job.needs) {
                        Needs.NOTHING -> NetworkType.NOT_REQUIRED
                        Needs.NETWORK -> NetworkType.CONNECTED
                        Needs.UNMETERED -> NetworkType.UNMETERED
                    },
                ).build()
                if (job.every != null) {
                    val req = PeriodicWorkRequestBuilder<ShuttleWorker>(job.every).setConstraints(constraints)
                        .addTag(TAG).addTag(job.name).build()
                    wm.enqueueUniquePeriodicWork(job.name, ExistingPeriodicWorkPolicy.UPDATE, req)
                } else {
                    val req = OneTimeWorkRequestBuilder<ShuttleWorker>().setConstraints(constraints)
                        .setInitialDelay(job.delay ?: java.time.Duration.ZERO).addTag(TAG).addTag(job.name).build()
                    wm.enqueueUniqueWork(job.name, ExistingWorkPolicy.KEEP, req)
                }
            }
            val keep = jobs.map { it.name }.toSet()
            for (info in wm.getWorkInfosByTag(TAG).get()) {
                if (info.state.isFinished || info.state == WorkInfo.State.RUNNING) continue
                val name = info.tags.firstOrNull { it.startsWith(JOB_PREFIX) } ?: continue
                if (name !in keep) wm.cancelUniqueWork(name)
            }
        }

        private const val TAG = "shuttle"
    }
}

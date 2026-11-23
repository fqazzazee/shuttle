// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ManualRunTest {
    private var waits = 0

    /** A WaitQuiet that times out until the given wait, as the binding's does. */
    private fun quietOnWait(n: Int): (Long) -> Unit = {
        if (++waits < n) error("the shuttles did not finish in time")
    }

    @Test
    fun aRunLongerThanOneWaitIsSeenThrough() {
        waitWhileBusy(quietOnWait(200), running = { true }, destroyed = { false })
        assertEquals(200, waits)
    }

    @Test
    fun aDestroyedServiceStopsWaiting() {
        waitWhileBusy(quietOnWait(Int.MAX_VALUE), running = { true }, destroyed = { waits >= 3 })
        assertEquals(3, waits)
    }

    @Test
    fun nothingRunningIsNotWaitedForForever() {
        waitWhileBusy(quietOnWait(Int.MAX_VALUE), running = { false }, destroyed = { false }, idleSteps = 5)
        assertEquals(5, waits)
    }

    @Test
    fun aRunStartingLateCountsAsBusyAgain() {
        // Idle for four of five allowed steps, then running, then idle again.
        waitWhileBusy(quietOnWait(Int.MAX_VALUE), running = { waits == 4 }, destroyed = { false }, idleSteps = 5)
        assertEquals(9, waits)
    }

    @Test
    fun aSyncThatFailsEndsTheRun() {
        val runs = AtomicInteger(1)
        val log = mutableListOf<String>()
        seeThrough(
            runs,
            sync = { error("unknown shuttle") },
            wait = { log += "wait" },
            failed = { log += "failed: ${it.message}" },
            ended = { log += "ended at ${runs.get()}" },
            background = { log += "background"; it() },
        )
        assertEquals(listOf("failed: unknown shuttle", "ended at 0"), log)
    }

    @Test
    fun aRunEndsAfterItsWait() {
        val runs = AtomicInteger(1)
        val log = mutableListOf<String>()
        seeThrough(
            runs,
            sync = { log += "sync" },
            wait = { log += "wait at ${runs.get()}" },
            failed = { log += "failed" },
            ended = { log += "ended at ${runs.get()}" },
            background = { it() },
        )
        assertEquals(listOf("sync", "wait at 1", "ended at 0"), log)
    }

    @Test
    fun aWaitThatFailsStillEndsTheRun() {
        val runs = AtomicInteger(1)
        var ended = false
        runCatching {
            seeThrough(
                runs,
                sync = {},
                wait = { error("binding") },
                failed = {},
                ended = { ended = true },
                background = { it() },
            )
        }
        assertEquals(0, runs.get())
        assertTrue(ended)
    }

    @Test
    fun readsWhetherAnyShuttleIsRunning() {
        assertTrue(anyRunning("""[{"Name":"a","State":"idle"},{"Name":"b","State":"running"}]"""))
        assertFalse(anyRunning("""[{"Name":"a","State":"idle"}]"""))
        assertFalse(anyRunning("[]"))
    }
}

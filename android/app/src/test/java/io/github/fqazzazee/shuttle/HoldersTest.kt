// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class HoldersTest {
    private val log = mutableListOf<String>()
    private var n = 0
    private val h = Holders<String>(stop = { log += "stop $it" })
    private val start = { "daemon${++n}".also { log += "start $it" } }

    @Test
    fun oneThingWhileAnyoneHoldsIt() {
        val a = h.acquire("screen", start)
        assertSame(a, h.acquire("service", start))
        assertSame(a, h.acquire("screen", start)) // the same holder twice is still one
        h.release("screen")
        assertEquals("daemon1", h.current)
        h.release("service")
        assertNull(h.current)
        assertEquals(listOf("start daemon1", "stop daemon1"), log)

        assertEquals("daemon2", h.acquire("worker", start)) // and again, a new one
    }

    @Test
    fun releasingWhatIsNotHeldStopsNothing() {
        h.acquire("screen", start)
        h.release("worker")
        h.release("worker")
        assertEquals("daemon1", h.current)
        assertEquals(listOf("start daemon1"), log)
    }

    @Test
    fun aFailedStartHoldsNothing() {
        val failing = Holders<String>(stop = {})
        runCatching { failing.acquire("screen") { error("bad config") } }
        assertNull(failing.current)
    }

    /** Reads current from another thread, as the main thread does, and says whether it came back. */
    private fun <T : Any> readsWhileBusy(holders: Holders<T>): Boolean {
        val read = CountDownLatch(1)
        Thread {
            holders.current
            read.countDown()
        }.apply { isDaemon = true }.start()
        return read.await(2, TimeUnit.SECONDS)
    }

    @Test
    fun currentDoesNotWaitForAStop() {
        val stopping = CountDownLatch(1)
        val finish = CountDownLatch(1)
        val slow = Holders<String>(stop = {
            stopping.countDown()
            finish.await()
        })
        slow.acquire("screen") { "daemon" }
        val releasing = Thread { slow.release("screen") }.apply { start() }
        stopping.await()
        try {
            assertTrue("current blocked on a stop in progress", readsWhileBusy(slow))
            assertNull(slow.current) // what is stopping is no longer there to be used
        } finally {
            finish.countDown()
            releasing.join()
        }
    }

    @Test
    fun currentDoesNotWaitForAStart() {
        val starting = CountDownLatch(1)
        val finish = CountDownLatch(1)
        val slow = Holders<String>(stop = {})
        val acquiring = Thread {
            slow.acquire("screen") {
                starting.countDown()
                finish.await()
                "daemon"
            }
        }.apply { start() }
        starting.await()
        try {
            assertTrue("current blocked on a start in progress", readsWhileBusy(slow))
        } finally {
            finish.countDown()
            acquiring.join()
        }
        assertEquals("daemon", slow.current)
    }

    @Test
    fun aFailedStopHoldsNothing() {
        val failing = Holders<String>(stop = { error("stop failed") })
        failing.acquire("screen") { "daemon" }
        runCatching { failing.release("screen") }
        assertNull(failing.current)
    }
}

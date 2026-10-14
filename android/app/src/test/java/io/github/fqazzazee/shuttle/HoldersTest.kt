// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
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
}

// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ShuttleStatusTest {
    // As the daemon writes it (daemon.Status through encoding/json), trimmed of fields the list does not read.
    private val idle = """{"Name":"photos","Mode":"push","State":"idle","Paused":false,"Metered":false,"Waiting":"",
        "Pending":false,"Share":"online","LastRun":"2026-10-14T18:30:00.25-04:00","Error":"",
        "Conflicts":[{"Path":"a"},{"Path":"b"}],"Errors":null,"Refused":[{"Path":"a:b"}],
        "Progress":{"Phase":"","Done":0,"Total":0,"Bytes":0,"TotalBytes":0,"Path":"","RateUp":0,"RateDown":0}}"""

    @Test
    fun anIdleShuttle() {
        val s = ShuttleStatus.parse(idle)
        assertEquals("photos", s.name)
        assertEquals("↑", s.arrow)
        assertEquals(Instant.parse("2026-10-14T22:30:00.25Z"), s.lastRun)
        assertEquals(2, s.conflicts)
        assertEquals(0, s.errors)
        assertEquals(1, s.refused)
        assertNull("no progress unless running", s.progress)
    }

    @Test
    fun neverRunIsNoTime() {
        val s = ShuttleStatus.parse(idle.replace("2026-10-14T18:30:00.25-04:00", "0001-01-01T00:00:00Z"))
        assertNull(s.lastRun)
    }

    @Test
    fun aRunningShuttlesProgress() {
        val s = ShuttleStatus.parse(
            idle.replace("\"State\":\"idle\"", "\"State\":\"running\"").replace("\"Mode\":\"push\"", "\"Mode\":\"sync\"")
                .replace("\"TotalBytes\":0", "\"TotalBytes\":400").replace("\"Bytes\":0", "\"Bytes\":100")
                .replace("\"RateUp\":0", "\"RateUp\":2048"),
        )
        assertEquals("⇅", s.arrow)
        assertEquals(0.25f, s.progress!!.fraction!!, 0f)
        assertEquals(2048L, s.progress!!.rateUp)
    }

    @Test
    fun progressByFilesThenUnknown() {
        assertEquals(0.5f, Progress("scanning", 5, 10, 0, 0, "", 0, 0).fraction!!, 0f)
        assertNull(Progress("scanning", 0, 0, 0, 0, "", 0, 0).fraction)
        assertEquals("↓", ShuttleStatus.parse(idle.replace("\"Mode\":\"push\"", "\"Mode\":\"pull\"")).arrow)
    }
}

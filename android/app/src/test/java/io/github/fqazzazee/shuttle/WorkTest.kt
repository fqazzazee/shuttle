// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.time.Duration
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Test

class WorkTest {
    private val now = Instant.parse("2026-10-14T22:00:00Z")

    @Test
    fun parsesTheBindingsWork() {
        val json = """[{"Name":"photos","NextRun":"2026-10-15T00:30:00.5-04:00","ShareAvailable":false,"Network":true,"Unmetered":true},
            {"Name":"card","ShareAvailable":true,"Network":false,"Unmetered":false}]"""
        assertEquals(
            listOf(
                Work("photos", Instant.parse("2026-10-15T04:30:00.5Z"), shareAvailable = false, network = true, unmetered = true),
                Work("card", null, shareAvailable = true, network = false, unmetered = false),
            ),
            parseWork(json),
        )
        assertEquals(emptyList<Work>(), parseWork("[]"))
    }

    @Test
    fun aScheduleIsOneJobAtItsTime() {
        val at = now.plus(Duration.ofHours(2))
        assertEquals(
            listOf(Job("shuttle:schedule:photos@${at.epochSecond}", Needs.NETWORK, Duration.ofHours(2), null)),
            planWork(listOf(Work("photos", at, shareAvailable = false, network = true, unmetered = false)), now),
        )
    }

    @Test
    fun aDueScheduleRunsAtOnce() {
        val past = now.minus(Duration.ofMinutes(5))
        assertEquals(Duration.ZERO, planWork(listOf(Work("p", past, false, true, false)), now).single().delay)
    }

    @Test
    fun aShareCheckEveryFifteenMinutes() {
        assertEquals(
            listOf(Job("shuttle:share:nas", Needs.UNMETERED, null, Duration.ofMinutes(15))),
            planWork(listOf(Work("nas", null, shareAvailable = true, network = true, unmetered = true)), now),
        )
    }

    @Test
    fun theNetworkAJobWaitsFor() {
        fun needs(network: Boolean, unmetered: Boolean) =
            planWork(listOf(Work("s", null, true, network, unmetered)), now).single().needs
        assertEquals(Needs.NETWORK, needs(network = true, unmetered = false))
        assertEquals(Needs.UNMETERED, needs(network = true, unmetered = true))
        // An SD card's shuttle waits for no network, metered or not.
        assertEquals(Needs.NOTHING, needs(network = false, unmetered = true))
    }

    @Test
    fun bothTriggersBothJobs() {
        val at = now.plus(Duration.ofHours(1))
        assertEquals(
            listOf("shuttle:schedule:s@${at.epochSecond}", "shuttle:share:s"),
            planWork(listOf(Work("s", at, true, true, false)), now).map { it.name },
        )
    }
}

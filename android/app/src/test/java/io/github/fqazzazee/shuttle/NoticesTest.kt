// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import org.junit.Assert.assertEquals
import org.junit.Test

class NoticesTest {
    @Test
    fun parsesTheCoresNote() {
        val json = """{"Key":"error/docs","Kind":"errors","Summary":"docs: sync failed","Body":"2 paths could not be synced\nx: busy","Open":""}"""
        assertEquals(Notice("error/docs", "errors", "docs: sync failed", "2 paths could not be synced\nx: busy"), Notice.parse(json))
    }

    @Test
    fun eachKindItsChannel() {
        assertEquals(Channel.ERRORS, Channel.of("errors"))
        assertEquals(Channel.CONFLICTS, Channel.of("conflicts"))
        assertEquals(Channel.COMPLETED, Channel.of("completed"))
        assertEquals(Channel.SHARE, Channel.of("share"))
        // Never into the service's quiet channel, and an unknown kind is seen, not lost.
        assertEquals(Channel.ERRORS, Channel.of("service"))
        assertEquals(Channel.ERRORS, Channel.of("something new"))
    }

    @Test
    fun aKeyReplacesNoKeyStacks() {
        assertEquals("share/nas", tagOf(Notice("share/nas", "share", "", ""), 1))
        assertEquals("share/nas", tagOf(Notice("share/nas", "share", "", ""), 2))
        assertEquals("conflicts#1", tagOf(Notice("", "conflicts", "", ""), 1))
        assertEquals("conflicts#2", tagOf(Notice("", "conflicts", "", ""), 2))
    }
}

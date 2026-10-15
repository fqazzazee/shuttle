// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.test.ext.junit.runners.AndroidJUnit4
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class WhenTest {
    private val now = Instant.parse("2026-10-15T12:00:00Z").toEpochMilli()

    @Test
    fun justNowForTheLastMinute() {
        assertEquals("just now", ago(Instant.parse("2026-10-15T11:59:30Z"), "just now", now))
        assertNotEquals("just now", ago(Instant.parse("2026-10-15T11:55:00Z"), "just now", now))
    }
}

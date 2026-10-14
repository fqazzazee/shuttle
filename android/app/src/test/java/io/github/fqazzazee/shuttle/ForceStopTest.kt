// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.app.ApplicationExitInfo.REASON_LOW_MEMORY
import android.app.ApplicationExitInfo.REASON_PACKAGE_UPDATED
import android.app.ApplicationExitInfo.REASON_USER_REQUESTED
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ForceStopTest {
    @Test
    fun aForceStopSinceTheLastLook() {
        val exits = listOf(Exit(300, REASON_LOW_MEMORY), Exit(200, REASON_USER_REQUESTED), Exit(100, REASON_PACKAGE_UPDATED))
        assertTrue(forceStoppedSince(exits, lastSeen = 0))
        assertTrue(forceStoppedSince(exits, lastSeen = 199))
        assertFalse("already said", forceStoppedSince(exits, lastSeen = 200))
        assertEquals(300, newestExit(exits, lastSeen = 0))
    }

    @Test
    fun otherEndsAreNotForceStops() {
        // Android ends a process for memory, or to update the app: the service comes back by itself.
        assertFalse(forceStoppedSince(listOf(Exit(5, REASON_LOW_MEMORY), Exit(6, REASON_PACKAGE_UPDATED)), lastSeen = 0))
        assertFalse(forceStoppedSince(emptyList(), lastSeen = 0))
        assertEquals(42, newestExit(emptyList(), lastSeen = 42))
    }
}

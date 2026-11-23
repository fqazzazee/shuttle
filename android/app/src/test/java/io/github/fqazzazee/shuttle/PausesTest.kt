// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class PausesTest {
    private val pauses = Pauses(
        ApplicationProvider.getApplicationContext<Context>().getSharedPreferences("test", Context.MODE_PRIVATE),
    )

    @Test
    fun aRemovedShuttleLeavesNoPauseForTheNextOfItsName() {
        pauses.set("photos", true)
        pauses.set("music", true)
        pauses.forget(setOf("photos", "never-paused"))
        assertEquals(setOf("music"), pauses.paused)
        assertEquals("""["music"]""", pauses.json())
    }

    @Test
    fun forgettingLeavesPauseAllAlone() {
        pauses.setAll(true)
        pauses.forget(setOf("photos"))
        assertTrue(pauses.all)
    }

    @Test
    fun readsWhatAReloadRemoved() {
        assertEquals(setOf("a", "b"), removedBy("""{"Removed":["a","b"]}"""))
        // Go writes an empty list as null.
        assertEquals(emptySet<String>(), removedBy("""{"Removed":null}"""))
        assertEquals(emptySet<String>(), removedBy("{}"))
    }
}

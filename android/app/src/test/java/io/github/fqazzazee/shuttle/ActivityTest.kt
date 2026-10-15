// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.time.Instant
import java.time.ZoneOffset
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ActivityTest {
    @get:Rule
    val compose = createComposeRule()

    // As the binding's ActivityJSON writes it.
    private val json = """[
        {"time":"2026-10-15T11:59:00Z","shuttle":"photos","kind":"run","text":"2 copied"},
        {"time":"2026-10-15T11:58:00Z","shuttle":"photos","kind":"done","path":"a.jpg","text":"copied to the share"},
        {"time":"2026-10-15T11:57:00Z","shuttle":"docs","kind":"error","path":"b.odt","text":"could not copy: permission denied"},
        {"time":"2026-10-15T11:56:00Z","shuttle":"docs","kind":"left-out","path":"c?.txt","text":"the local folder cannot hold this name"}]"""
    private val now = Instant.parse("2026-10-15T12:00:00Z")

    @Test
    fun parsesTheBindingsEntries() {
        val es = Entry.parseAll(json)
        assertEquals(listOf("photos", "photos", "docs", "docs"), es.map { it.shuttle })
        assertEquals(listOf(false, false, true, true), es.map { it.problem })
        assertEquals("", es[0].path)
    }

    @Test
    fun theTuisStamps() {
        val utc = ZoneOffset.UTC
        assertEquals("11:59:00", stamp(Instant.parse("2026-10-15T11:59:00Z"), now, utc))
        assertEquals("Oct 2 09:00:00", stamp(Instant.parse("2026-10-02T09:00:00Z"), now, utc))
        assertEquals("2025-12-31 23:00", stamp(Instant.parse("2025-12-31T23:00:00Z"), now, utc))
    }

    @Test
    fun everyShuttleOrOneAndProblemsOnly() {
        val picked = mutableListOf<String>()
        var problems = false
        compose.setContent {
            ActivityScreen(listOf("docs", "photos"), Entry.parseAll(json), "", { picked += it }, problems, { problems = it }, now)
        }
        compose.onNodeWithText("a.jpg: copied to the share").assertExists()
        compose.onNodeWithText("b.odt: could not copy: permission denied").assertExists()
        compose.onNodeWithText("photos").performClick()
        assertEquals(listOf("photos"), picked)
        compose.onNodeWithText("Problems only").performClick()
        assertTrue(problems)
    }

    @Test
    fun problemsOnlyLeavesWorkDoneOut() {
        compose.setContent { ActivityScreen(listOf("docs"), Entry.parseAll(json), "", {}, true, {}, now) }
        compose.onNodeWithText("a.jpg: copied to the share").assertDoesNotExist()
        compose.onNodeWithText("c?.txt: the local folder cannot hold this name").assertExists()
    }

    @Test
    fun noProblemsIsNotNothingYet() {
        val done = Entry.parseAll(json).filter { !it.problem }
        compose.setContent { ActivityScreen(listOf("photos"), done, "", {}, true, {}, now) }
        compose.onNodeWithText("No problems in the activity shown.").assertExists()
    }

    @Test
    fun readingAndNothingYet() {
        compose.setContent { ActivityScreen(emptyList(), null, "", {}, false, {}, now) }
        compose.onNodeWithText("Reading the activity…").assertExists()
    }
}

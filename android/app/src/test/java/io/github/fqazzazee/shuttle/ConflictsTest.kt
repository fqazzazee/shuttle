// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ConflictsTest {
    @get:Rule
    val compose = createComposeRule()

    private fun file(size: Int) = """{"Name":"x","Kind":0,"Size":$size,"ModTime":"2026-10-15T10:00:00Z"}"""
    private val dir = """{"Name":"x","Kind":1,"Size":0,"ModTime":"2026-10-15T10:00:00Z"}"""

    // As the daemon's statuses have them.
    private val statuses = mapOf(
        "photos" to """{"Name":"photos","Conflicts":[
            {"Path":"a.jpg","Op":9,"Reason":"changed on both sides","Outcome":"held","Local":${file(5)},"Remote":${file(15)}},
            {"Path":"b.jpg","Op":9,"Reason":"changed on both sides","Outcome":"kept both: …","Local":${file(1)},"Remote":${file(2)}},
            {"Path":"x","Op":9,"Reason":"a file here, a folder on the share","Outcome":"held","Local":${file(3)},"Remote":$dir}]}""",
        "docs" to """{"Name":"docs","Conflicts":[
            {"Path":"Readme.md","Op":9,"Reason":"differs only in case from README.md","Outcome":"held","Local":null,"Remote":${file(9)}}]}""",
        "music" to """{"Name":"music","Conflicts":null,"HeldDeletions":15}""",
    )

    @Test
    fun onlyHeldOnesShuttleByShuttle() {
        val items = ConflictItem.held(statuses)
        assertEquals(listOf("docs" to "Readme.md", "photos" to "a.jpg", "photos" to "x"), items.map { it.shuttle to it.path })
        assertEquals(15L, items[1].remote!!.size)
    }

    @Test
    fun theTuisChoices() {
        val (case, files, kinds) = ConflictItem.held(statuses)
        assertEquals(emptyList<Keep>(), case.choices)
        assertEquals(Keep.entries, files.choices)
        assertEquals(listOf(Keep.BOTH), kinds.choices)
    }

    @Test
    fun settlesFromItsCard() {
        val done = mutableListOf<Pair<String, Keep>>()
        compose.setContent { ConflictsScreen(ConflictItem.held(statuses), emptyList(), null, { c, k -> done += c.path to k }) {} }
        compose.onNodeWithText("Rename one of them yourself: the phone cannot keep both names.").assertExists()
        compose.onNodeWithText("Keep the share's").performScrollTo().performClick()
        assertEquals(listOf("a.jpg" to Keep.REMOTE), done)
    }

    @Test
    fun noneWaiting() {
        compose.setContent { ConflictsScreen(emptyList(), emptyList(), null, { _, _ -> }) {} }
        compose.onNodeWithText("No conflicts waiting for you.").assertExists()
    }

    @Test
    fun heldDeletionsAndTheirAllow() {
        assertEquals(listOf(HeldDeletions("music", 15)), HeldDeletions.of(statuses))
        val allowed = mutableListOf<String>()
        compose.setContent { ConflictsScreen(emptyList(), HeldDeletions.of(statuses), null, { _, _ -> }) { allowed += it } }
        compose.onNodeWithText("music: 15 deletions held back").assertExists()
        compose.onNodeWithText("No conflicts waiting for you.").assertDoesNotExist()
        compose.onNodeWithText("Allow them").performScrollTo().performClick()
        assertEquals(listOf("music"), allowed)
    }
}

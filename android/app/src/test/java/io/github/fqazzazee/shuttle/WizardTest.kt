// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.state.ToggleableState
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assert
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsEnabled
import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isToggleable
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
class WizardTest {
    @get:Rule
    val compose = createComposeRule()

    // As the binding's BrowseJSON writes it.
    private val json = """[{"Name":"Music","Dir":true},{"Name":"Photos","Dir":true},{"Name":"a.txt","Dir":false}]"""

    @Test
    fun parsesTheBindingsListing() {
        assertEquals(listOf(FolderEntry("Music", true), FolderEntry("Photos", true), FolderEntry("a.txt", false)), FolderEntry.parseAll(json))
    }

    @Test
    fun aFolderPathGoesInAndUp() {
        val top = WizardState(share = "nas")
        assertEquals("nas:/Photos/2024", top.into("Photos").into("2024").remote)
        assertEquals("/Photos", top.into("Photos").into("2024").up().dir)
        assertEquals("/", top.into("Photos").up().dir)
    }

    @Test
    fun aShareIsChosenBeforeNext() {
        val shares = listOf(ShareRow("nas", "smb", "nas.example/home", emptyList(), "", "", false))
        var chosen = ""
        var next = 0
        compose.setContent { WizardShareStep(shares, chosen, { chosen = it }, { next++ }) {} }
        compose.onNodeWithText("Next").assertIsNotEnabled()
        compose.onNodeWithText("smb · nas.example/home").assertExists()
        compose.onNodeWithText("nas").performClick()
        assertEquals("nas", chosen)
    }

    @Test
    fun noShareYet() {
        compose.setContent { WizardShareStep(emptyList(), "", {}, {}) {} }
        compose.onNodeWithText("There is no share yet: add one on the Shares tab.").assertExists()
    }

    // Folders open, files only show; Up goes back to the folder above.
    @Test
    fun theFolderIsBrowsed() {
        val opened = mutableListOf<String>()
        var next = 0
        val state = WizardState(WizardStep.FOLDER, "nas", "/Media")
        compose.setContent { WizardFolderStep(state, Listing(FolderEntry.parseAll(json), null), { opened += it.dir }, {}, {}) { next++ } }
        compose.onNodeWithText("nas:/Media").assertExists()
        compose.onNodeWithText("Photos/").performClick()
        compose.onNodeWithText("↑ Up a folder").performClick()
        compose.onNode(hasText("a.txt") and hasClickAction()).assertIsNotEnabled()
        compose.onNodeWithText("Use this folder").assertIsEnabled().performScrollTo().performClick()
        assertEquals(listOf("/Media/Photos", "/"), opened)
        assertEquals(1, next)
    }

    @Test
    fun aFolderThatCouldNotBeReadIsTriedAgainAndNotChosen() {
        var tries = 0
        compose.setContent {
            WizardFolderStep(WizardState(WizardStep.FOLDER, "nas"), Listing(null, "connection refused"), {}, { tries++ }, {}) {}
        }
        compose.onNodeWithText("↑ Up a folder").assertDoesNotExist()
        compose.onNodeWithText("connection refused").assertExists()
        compose.onNodeWithText("Use this folder").assertIsNotEnabled()
        compose.onNodeWithText("Try again").performClick()
        assertEquals(1, tries)
    }

    @Test
    fun whileReadingAndEmpty() {
        compose.setContent { WizardFolderStep(WizardState(WizardStep.FOLDER, "nas"), null, {}, {}, {}) {} }
        compose.onNodeWithText("Reading the folder…").assertExists()
        compose.onNodeWithText("Use this folder").assertIsNotEnabled()
    }

    @Test
    fun anEmptyFolder() {
        compose.setContent { WizardFolderStep(WizardState(WizardStep.FOLDER, "nas"), Listing(emptyList(), null), {}, {}, {}) {} }
        compose.onNodeWithText("Nothing in this folder.").assertExists()
        compose.onNodeWithText("Use this folder").assertIsEnabled()
    }

    @Test
    fun pathsInsideTheFolder() {
        assertEquals(true, within("/a/b", "/a"))
        assertEquals(false, within("/ab", "/a"))
        assertEquals(true, within("/x", "/"))
        assertEquals("/a/b", child("/a", "b"))
        assertEquals("/b", child("/", "b"))
    }

    private fun view(mode: Int, marks: List<String> = emptyList(), synced: Map<String, Int> = emptyMap()) =
        MarksView(mode, { synced[it] ?: if (mode == 0) 2 else 0 }, marks)

    // Ticks mark by full path; a folder opens inside the shuttle's folder; what each tick shows
    // comes from the picker.
    @Test
    fun whatToSyncIsMarked() {
        val toggled = mutableListOf<String>()
        val opened = mutableListOf<String>()
        val modes = mutableListOf<Int>()
        val state = WizardState(WizardStep.WHAT, "nas", "/Media", "/Media")
        val v = view(1, listOf("Photos/"), mapOf("/Media/Photos" to 2, "/Media/Music" to 1))
        compose.setContent {
            WizardWhatStep(state, Listing(FolderEntry.parseAll(json), null), v, { modes += it }, { toggled += it }, { opened += it.inner }, {}, {}) {}
        }
        compose.onNodeWithText("Synced only: Photos/").assertExists()
        compose.onNodeWithText("↑ Up a folder").assertDoesNotExist() // the shuttle's folder is the top here
        compose.onNode(hasText("Photos/") and hasClickAction()).performClick()
        compose.onNodeWithText("All but what is marked").performClick()
        compose.onAllNodes(isToggleable())[0] // Music: something in it
            .assert(SemanticsMatcher.expectValue(SemanticsProperties.ToggleableState, ToggleableState.Indeterminate))
        compose.onAllNodes(isToggleable())[1].assertIsOn() // Photos: all of it
        compose.onAllNodes(isToggleable())[2].assertIsOff().performScrollTo().performClick() // a.txt
        assertEquals(listOf("/Media/a.txt"), toggled)
        assertEquals(listOf("/Media/Photos"), opened)
        assertEquals(listOf(2), modes)
        compose.onNodeWithText("Next").performScrollTo().assertIsEnabled()
    }

    @Test
    fun onlyWhatIsMarkedNeedsAMark() {
        compose.setContent {
            WizardWhatStep(WizardState(WizardStep.WHAT, "nas", "/Media", "/Media"), Listing(emptyList(), null), view(1), {}, {}, {}, {}, {}) {}
        }
        compose.onNodeWithText("Nothing marked yet: tick what to sync.").assertExists()
        compose.onNodeWithText("Next").performScrollTo().assertIsNotEnabled()
    }

    // Everything: no ticks, and folders still open, to look.
    @Test
    fun everythingHasNoTicks() {
        val opened = mutableListOf<String>()
        val state = WizardState(WizardStep.WHAT, "nas", "/Media", "/Media/Old")
        compose.setContent {
            WizardWhatStep(state, Listing(FolderEntry.parseAll(json), null), view(0), {}, {}, { opened += it.inner }, {}, {}) {}
        }
        compose.onAllNodes(isToggleable()).assertCountEquals(0)
        compose.onNodeWithText("nas:/Media/Old").assertExists()
        compose.onNodeWithText("↑ Up a folder").performClick()
        assertEquals(listOf("/Media"), opened)
        compose.onNodeWithText("Next").performScrollTo().assertIsEnabled()
    }
}

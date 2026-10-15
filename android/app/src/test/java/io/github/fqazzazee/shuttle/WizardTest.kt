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
import androidx.compose.ui.test.performTextClearance
import androidx.compose.ui.test.performTextInput
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
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

    // A name and a share before Next; why Next did not go on shows.
    @Test
    fun aNameAndAShareBeforeNext() {
        val shares = listOf(ShareRow("nas", "smb", "nas.example/home", emptyList(), "", "", false))
        var chosen by mutableStateOf("")
        var name by mutableStateOf("")
        var next = 0
        compose.setContent { WizardShareStep(shares, chosen, name, "there is a shuttle named photos already", { name = it }, { chosen = it }, { next++ }) {} }
        compose.onNodeWithText("Next").assertIsNotEnabled()
        compose.onNodeWithText("smb · nas.example/home").assertExists()
        compose.onNodeWithText("nas").performClick()
        assertEquals("nas", chosen)
        compose.onNodeWithText("Next").assertIsNotEnabled()
        compose.onNodeWithText("The shuttle's name").performTextInput("photos")
        compose.onNodeWithText("there is a shuttle named photos already").assertExists()
        compose.onNodeWithText("Next").assertIsEnabled().performClick()
        assertEquals(1, next)
    }

    @Test
    fun noShareYet() {
        compose.setContent { WizardShareStep(emptyList(), "", "", null, {}, {}, {}) {} }
        compose.onNodeWithText("There is no share yet: add one on the Shares tab.").assertExists()
    }

    // Folders open, files only show; Up goes back to the folder above.
    @Test
    fun theFolderIsBrowsed() {
        val opened = mutableListOf<String>()
        var next = 0
        val state = WizardState(WizardStep.FOLDER, "nas", "/Media")
        compose.setContent { WizardFolderStep(state, Listing(FolderEntry.parseAll(json), null), { opened += it.dir }, { null }, {}, {}) { next++ } }
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
            WizardFolderStep(WizardState(WizardStep.FOLDER, "nas"), Listing(null, "connection refused"), {}, { null }, { tries++ }, {}) {}
        }
        compose.onNodeWithText("↑ Up a folder").assertDoesNotExist()
        compose.onNodeWithText("connection refused").assertExists()
        compose.onNodeWithText("Use this folder").assertIsNotEnabled()
        compose.onNodeWithText("Try again").performClick()
        assertEquals(1, tries)
    }

    @Test
    fun whileReadingAndEmpty() {
        compose.setContent { WizardFolderStep(WizardState(WizardStep.FOLDER, "nas"), null, {}, { null }, {}, {}) {} }
        compose.onNodeWithText("Reading the folder…").assertExists()
        compose.onNodeWithText("Use this folder").assertIsNotEnabled()
    }

    @Test
    fun anEmptyFolder() {
        compose.setContent { WizardFolderStep(WizardState(WizardStep.FOLDER, "nas"), Listing(emptyList(), null), {}, { null }, {}, {}) {} }
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

    private val phone = Volume("Internal storage", "/storage/emulated/0")
    private val card = Volume("SD card", "/storage/1234-ABCD")

    @Test
    fun theTopOfStorageIsNotAShuttlesFolder() {
        assertEquals(true, storageTop("/storage/emulated/0/", listOf(phone)))
        assertEquals(true, storageTop("/sdcard", listOf(phone)))
        assertEquals(true, storageTop("/storage/1234-ABCD", listOf(phone, card)))
        assertEquals(false, storageTop("/storage/emulated/0/DCIM", listOf(phone)))
    }

    // As the share's folder: folders open, files only show, Up goes to the folder above.
    @Test
    fun thePhonesFolderIsBrowsed() {
        val opened = mutableListOf<String>()
        var next = 0
        val state = WizardState(WizardStep.LOCAL, "nas", "/Media", local = "/storage/emulated/0/Media")
        compose.setContent { WizardLocalStep(state, listOf(phone), Listing(FolderEntry.parseAll(json), null), null, { opened += it }, { null }, {}, {}) { next++ } }
        compose.onNodeWithText("/storage/emulated/0/Media").assertExists()
        compose.onNodeWithText("Photos/").performClick()
        compose.onNodeWithText("↑ Up a folder").performClick()
        compose.onNode(hasText("a.txt") and hasClickAction()).assertIsNotEnabled()
        compose.onNodeWithText("Use this folder").performScrollTo().assertIsEnabled().performClick()
        assertEquals(listOf("/storage/emulated/0/Media/Photos", "/storage/emulated/0"), opened)
        assertEquals(1, next)
    }

    // At the top of the phone's storage: no Up, the SD card instead, and nothing to choose.
    @Test
    fun theTopOfStorageGoesToTheCard() {
        val opened = mutableListOf<String>()
        val state = WizardState(WizardStep.LOCAL, "nas", "/Media", local = "/storage/emulated/0")
        compose.setContent { WizardLocalStep(state, listOf(phone, card), Listing(FolderEntry.parseAll(json), null), null, { opened += it }, { null }, {}, {}) {} }
        compose.onNodeWithText("↑ Up a folder").assertDoesNotExist()
        compose.onNodeWithText("Internal storage (/storage/emulated/0)").assertDoesNotExist()
        compose.onNodeWithText("SD card (/storage/1234-ABCD)").performClick()
        assertEquals(listOf("/storage/1234-ABCD"), opened)
        compose.onNodeWithText("Choose a folder in it, or make one: a shuttle does not sync all of the phone's storage.").assertExists()
        compose.onNodeWithText("Use this folder").performScrollTo().assertIsNotEnabled()
    }

    // A refused name keeps the dialog open, saying why; a folder made closes it.
    @Test
    fun aFolderIsMade() {
        val asked = mutableListOf<String>()
        val state = WizardState(WizardStep.LOCAL, "nas", "/Media", local = "/storage/emulated/0/Media")
        compose.setContent {
            WizardLocalStep(state, listOf(phone), Listing(emptyList(), null), null, {}, { asked += it; if (it == "a:b") "the phone cannot hold that name" else null }, {}, {}) {}
        }
        compose.onNodeWithText("Nothing in this folder.").assertExists()
        compose.onNodeWithText("New folder").performScrollTo().performClick()
        compose.onNodeWithText("In /storage/emulated/0/Media").assertExists()
        compose.onNodeWithText("Name").performTextInput("a:b")
        compose.onNodeWithText("Make").performClick()
        compose.onNodeWithText("the phone cannot hold that name").assertExists()
        compose.onNodeWithText("Name").performTextClearance()
        compose.onNodeWithText("Name").performTextInput("Backups")
        compose.onNodeWithText("Make").performClick()
        compose.onNodeWithText("Make").assertDoesNotExist()
        assertEquals(listOf("a:b", "Backups"), asked)
    }

    // A folder that could not be read, as without All files access: tried again, not chosen, nothing made in it.
    @Test
    fun aPhoneFolderThatCouldNotBeRead() {
        var retried = 0
        val state = WizardState(WizardStep.LOCAL, "nas", "/Media", local = "/storage/emulated/0/Media")
        compose.setContent { WizardLocalStep(state, listOf(phone), Listing(null, "permission denied"), null, {}, { null }, { retried++ }, {}) {} }
        compose.onNodeWithText("permission denied").assertExists()
        compose.onNodeWithText("Try again").performClick()
        assertEquals(1, retried)
        compose.onNodeWithText("New folder").assertDoesNotExist()
        compose.onNodeWithText("Use this folder").performScrollTo().assertIsNotEnabled()
    }

    // Folders inside another shuttle's are said on the step that chose them.
    @Test
    fun aPhoneFolderRefused() {
        val state = WizardState(WizardStep.LOCAL, "nas", "/Media", local = "/storage/emulated/0/Media")
        compose.setContent { WizardLocalStep(state, listOf(phone), Listing(emptyList(), null), "shuttles.photos and shuttles.s: folders overlap", {}, { null }, {}, {}) {} }
        compose.onNodeWithText("shuttles.photos and shuttles.s: folders overlap").assertExists()
    }

    private val profiles = listOf(ProfileRow("live", "sync", "realtime", listOf("docs")), ProfileRow("quiet", "", "", emptyList()))

    @Test
    fun aProfileOrNone() {
        var chosen by mutableStateOf("")
        var next = 0
        compose.setContent { WizardProfileStep(profiles, chosen, { chosen = it }, {}) { next++ } }
        compose.onNodeWithText("No profile").assertExists()
        compose.onNodeWithText("sync · realtime").assertExists()
        compose.onNodeWithText("live").performClick()
        assertEquals("live", chosen)
        compose.onNodeWithText("Next").performScrollTo().performClick()
        assertEquals(1, next)
    }

    // The last step: its settings, whence the rest comes, and Save, which says why it did not.
    @Test
    fun theSettingsAndSave() {
        var saved = 0
        val rows = listOf(Setting("mode", "Mode", "", listOf("push", "pull", "sync"), false, "", "sync", "profile live", ""))
        compose.setContent {
            SettingsScreen("Settings of photos", rows, { _, _ -> null }, head = { WizardSaveHead("live", "shuttles.photos: mode is not set", { saved++ }) }) {}
        }
        compose.onNodeWithText("What it does not set comes from live. Tap a setting to give it one of its own.").assertExists()
        compose.onNodeWithText("shuttles.photos: mode is not set").assertExists()
        compose.onNodeWithText("Save the shuttle").performClick()
        assertEquals(1, saved)
        compose.onNodeWithText("Mode").assertExists()
    }

    // A saved shuttle keeps its name, and says whose wizard it is.
    @Test
    fun anEditKeepsItsName() {
        val shares = listOf(ShareRow("nas", "smb", "nas.example/home", emptyList(), "", "", false))
        compose.setContent {
            CompositionLocalProvider(LocalWizardEditing provides "photos") { WizardShareStep(shares, "nas", "photos", null, {}, {}, {}) {} }
        }
        compose.onNodeWithText("Edit photos").assertExists()
        compose.onNodeWithText("The shuttle's name").assertIsNotEnabled()
        compose.onNodeWithText("Next").assertIsEnabled()
    }

    @Test
    fun aMoveIsAskedAbout() {
        var saved = 0
        var cancelled = 0
        compose.setContent { MovedDialog("photos", { saved++ }) { cancelled++ } }
        compose.onNodeWithText("photos now syncs other folders", substring = true).assertExists()
        compose.onNodeWithText("Save").performClick()
        compose.onNodeWithText("Cancel").performClick()
        assertEquals(1, saved)
        assertEquals(1, cancelled)
    }

    // As on the phone: a folder made on the share, in the folder shown; a refusal keeps the dialog open.
    @Test
    fun aFolderIsMadeOnTheShare() {
        val asked = mutableListOf<String>()
        val state = WizardState(WizardStep.FOLDER, "nas", "/Media")
        compose.setContent {
            WizardFolderStep(state, Listing(emptyList(), null), {}, { asked += it; if (it == "a:b") "the share cannot hold that name" else null }, {}, {}) {}
        }
        compose.onNodeWithText("New folder").performScrollTo().performClick()
        compose.onNodeWithText("In nas:/Media").assertExists()
        compose.onNodeWithText("Name").performTextInput("a:b")
        compose.onNodeWithText("Make").performClick()
        compose.onNodeWithText("the share cannot hold that name").assertExists()
        compose.onNodeWithText("Name").performTextClearance()
        compose.onNodeWithText("Name").performTextInput("Backups")
        compose.onNodeWithText("Make").performClick()
        compose.onNodeWithText("Make").assertDoesNotExist()
        assertEquals(listOf("a:b", "Backups"), asked)
    }

    @Test
    fun noNewFolderInAFolderThatCouldNotBeRead() {
        compose.setContent { WizardFolderStep(WizardState(WizardStep.FOLDER, "nas"), Listing(null, "connection refused"), {}, { null }, {}, {}) {} }
        compose.onNodeWithText("New folder").assertDoesNotExist()
    }
}

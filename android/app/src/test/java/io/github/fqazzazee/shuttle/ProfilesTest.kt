// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ProfilesTest {
    @get:Rule
    val compose = createComposeRule()

    // As the binding's ProfilesJSON writes it.
    private val json = """[{"Name":"phone","Mode":"push","Triggers":"realtime","UsedBy":["photos","music"]},
        {"Name":"quiet","Mode":"","Triggers":"manual only","UsedBy":[]}]"""

    @Test
    fun parsesTheBindingsProfiles() {
        assertEquals(listOf(ProfileRow("phone", "push", "realtime", listOf("photos", "music")), ProfileRow("quiet", "", "manual only", emptyList())),
            ProfileRow.parseAll(json))
    }

    // As the binding's ProfileSettingsJSON writes them: the default is what a profile inherits.
    private val rows = """[
        {"Key":"mode","Label":"Mode","Hint":"","Choices":["push","pull","sync"],"List":false,"Number":false,
         "Value":"pull","Inherited":"none: each shuttle must say","From":"default","Off":""},
        {"Key":"conflicts","Label":"Conflicts","Hint":"","Choices":["keep-both","newest-wins","ask"],"List":false,"Number":false,
         "Value":"","Inherited":"keep-both","From":"default","Off":""}]"""

    private class Calls {
        val made = mutableListOf<String>()
        var refuse: String? = null
        val actions = ProfileActions(
            create = { n, m -> made += "create $n $m"; refuse },
            duplicate = { f, t -> made += "duplicate $f $t"; refuse },
            delete = { n -> made += "delete $n"; refuse },
        )
    }

    @Test
    fun eachProfileAsTheTuiListsIt() {
        val opened = mutableListOf<String>()
        compose.setContent { ProfilesScreen(ProfileRow.parseAll(json), null, { opened += it }) {} }
        for (text in listOf("phone", "push · realtime", "Used by photos, music", "quiet", "— · manual only", "No shuttle uses it")) {
            compose.onNodeWithText(text).assertExists()
        }
        compose.onNodeWithText("quiet").performClick()
        assertEquals(listOf("quiet"), opened)
    }

    @Test
    fun none() {
        compose.setContent { ProfilesScreen(emptyList(), null, {}) {} }
        compose.onNodeWithText("No profiles yet. A profile is a set of settings that shuttles share.").assertExists()
        compose.onNodeWithText("Add a profile").assertExists()
    }

    // A new profile needs a name and its mode before it can be saved.
    @Test
    fun aNewProfileIsNamedWithItsMode() {
        val c = Calls()
        compose.setContent { NewProfileDialog(c.actions.create) {} }
        compose.onNodeWithText("Save").assertIsNotEnabled()
        compose.onNodeWithText("Name").performTextInput("quiet")
        compose.onNodeWithText("Save").assertIsNotEnabled()
        compose.onNodeWithText("Pull from it").performClick()
        compose.onNodeWithText("Save").performClick()
        assertEquals(listOf("create quiet pull"), c.made)
    }

    @Test
    fun aRefusedNameIsShownAndTheDialogStays() {
        val c = Calls().apply { refuse = "there is a profile named quiet already" }
        compose.setContent { NewProfileDialog(c.actions.create) {} }
        compose.onNodeWithText("Name").performTextInput("quiet")
        compose.onNodeWithText("Sync both ways").performClick()
        compose.onNodeWithText("Save").performClick()
        compose.onNodeWithText("there is a profile named quiet already").assertExists()
    }

    // Its settings, as a shuttle's, from the default; a copy under a name of its own; and delete,
    // offered only while no shuttle uses it.
    @Test
    fun theEditorDuplicatesAndDeletes() {
        val c = Calls()
        compose.setContent { ProfileEditor("quiet", Setting.parseAll(rows), emptyList(), { _, _ -> null }, c.actions) {} }
        compose.onNodeWithText("The profile quiet").assertExists()
        compose.onNodeWithText("pull  ·  its own").assertExists()
        compose.onNodeWithText("keep-both  ·  from default").assertExists()

        compose.onNodeWithText("Duplicate").performClick()
        compose.onNodeWithText("Duplicate quiet as").assertExists()
        compose.onNodeWithText("quiet-copy").assertExists()
        compose.onNodeWithText("Save").performClick()
        compose.onNodeWithText("Delete").performClick()
        compose.onAllNodesWithText("Delete")[1].performClick() // the dialog's
        assertEquals(listOf("duplicate quiet quiet-copy", "delete quiet"), c.made)
    }

    @Test
    fun aProfileInUseIsNotOfferedForDeleting() {
        val c = Calls()
        compose.setContent { ProfileEditor("phone", Setting.parseAll(rows), listOf("photos"), { _, _ -> null }, c.actions) {} }
        compose.onNodeWithText("Used by photos").assertExists()
        compose.onNodeWithText("Delete").assertDoesNotExist()
    }
}

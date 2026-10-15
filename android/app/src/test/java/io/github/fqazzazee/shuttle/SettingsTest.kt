// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.hasClickAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class SettingsTest {
    @get:Rule
    val compose = createComposeRule()

    // As the binding's SettingsJSON writes it.
    private val json = """[
        {"Key":"conflicts","Label":"Conflicts","Hint":"","Choices":["keep-both","newest-wins","ask"],"List":false,"Number":false,
         "Value":"","Inherited":"keep-both","From":"default","Off":""},
        {"Key":"interval","Label":"Every","Hint":"e.g. 1h","Choices":null,"List":false,"Number":false,
         "Value":"","Inherited":"","From":"default","Off":"only with the schedule trigger"},
        {"Key":"metered","Label":"On a metered connection","Hint":"","Choices":["pause","run"],"List":false,"Number":false,
         "Value":"run","Inherited":"pause","From":"profile phone","Off":""}]"""

    @Test
    fun parsesTheBindingsRows() {
        val rows = Setting.parseAll(json)
        assertEquals(listOf("conflicts", "interval", "metered"), rows.map { it.key })
        assertEquals("keep-both", rows[0].shown)
        assertEquals("run", rows[2].shown)
        assertEquals(emptyList<String>(), rows[1].choices)
    }

    @Test
    fun aChoiceIsSetAndAnOwnValueReset() {
        val calls = mutableListOf<Pair<String, String>>()
        compose.setContent { SettingsScreen("photos", Setting.parseAll(json), { k, t -> calls += k to t; null }) {} }
        compose.onNodeWithText("keep-both  ·  from default").assertExists()
        compose.onNodeWithText("run  ·  its own").assertExists()
        compose.onNodeWithText("only with the schedule trigger").assertExists()
        compose.onNode(hasText("Every") and hasClickAction()).assertIsNotEnabled()

        compose.onNodeWithText("Conflicts").performClick()
        compose.onNodeWithText("ask").performClick()
        compose.onNodeWithText("Save").performClick()
        compose.onNodeWithText("On a metered connection").performClick()
        compose.onNodeWithText("Use profile phone's (pause)").performClick()
        assertEquals(listOf("conflicts" to "ask", "metered" to ""), calls)
    }

    @Test
    fun aRefusalIsShownAndTheEditorStays() {
        compose.setContent { SettingsScreen("photos", Setting.parseAll(json), { _, _ -> "must be one of keep-both, newest-wins, ask" }) {} }
        compose.onNodeWithText("Conflicts").performClick()
        compose.onNodeWithText("Save").performClick()
        compose.onNodeWithText("must be one of keep-both, newest-wins, ask").assertExists()
    }
}

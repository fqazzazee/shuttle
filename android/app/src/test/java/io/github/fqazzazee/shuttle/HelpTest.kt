// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class HelpTest {
    @get:Rule
    val compose = createComposeRule()

    private val list = SemanticsMatcher.keyIsDefined(SemanticsProperties.VerticalScrollAxisRange)

    // A question opens to its answer, and closes again; one at a time.
    @Test
    fun aQuestionOpensAndCloses() {
        var back = false
        compose.setContent { HelpScreen { back = true } }
        compose.onNodeWithText("How do I start?").performClick()
        compose.onNodeWithText("Give the app the three permissions it asks for", substring = true).assertExists()
        compose.onNodeWithText("Push, pull or sync?").performClick()
        compose.onNodeWithText("Give the app the three permissions it asks for", substring = true).assertDoesNotExist()
        compose.onNodeWithText("Push copies what changes on the phone", substring = true).assertExists()
        compose.onNodeWithText("Push, pull or sync?").performClick()
        compose.onNodeWithText("Push copies what changes on the phone", substring = true).assertDoesNotExist()
        compose.onNodeWithText("← Back").performClick()
        assertTrue(back)
    }

    // The answers' quotes, backslash and angle brackets come out as written.
    @Test
    fun theAnswersReadAsWritten() {
        compose.setContent { HelpScreen {} }
        compose.onNode(list).performScrollToNode(hasText("Why were some files left out?"))
        compose.onNodeWithText("Why were some files left out?").performClick()
        compose.onNode(list).performScrollToNode(hasText("The phone's storage", substring = true))
        compose.onNodeWithText("""cannot hold names with : ? * " < > | or \, which""", substring = true).assertExists()
        compose.onNode(list).performScrollToNode(hasText("Can a shuttle sync only some of the folder?"))
        compose.onNodeWithText("Can a shuttle sync only some of the folder?").performClick()
        compose.onNodeWithText("""on the wizard's "What to sync in it" step""", substring = true).assertExists()
    }
}

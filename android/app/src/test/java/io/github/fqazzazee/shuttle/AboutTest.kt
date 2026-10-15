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
class AboutTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun theVersionTheLicenseAndTheNotices() {
        var back = false
        val notices = listOf("Third-party notices for shuttle's Android app") + (1..200).map { "paragraph $it" }
        compose.setContent { AboutScreen("0.3.0", notices) { back = true } }
        compose.onNodeWithText("Version 0.3.0").assertExists()
        compose.onNodeWithText("PolyForm Noncommercial License 1.0.0", substring = true).assertExists()
        compose.onNodeWithText("Third-party notices for shuttle's Android app").assertExists()
        // The last of many, scrolled to.
        compose.onNode(SemanticsMatcher.keyIsDefined(SemanticsProperties.VerticalScrollAxisRange)).performScrollToNode(hasText("paragraph 200"))
        compose.onNodeWithText("paragraph 200").assertExists()
        compose.onNode(SemanticsMatcher.keyIsDefined(SemanticsProperties.VerticalScrollAxisRange)).performScrollToNode(hasText("← Back"))
        compose.onNodeWithText("← Back").performClick()
        assertTrue(back)
    }

    @Test
    fun aBuildWithoutNotices() {
        compose.setContent { AboutScreen("0.0.0-dev", null) {} }
        compose.onNodeWithText("This build was made without them: a release carries them.").assertExists()
    }
}

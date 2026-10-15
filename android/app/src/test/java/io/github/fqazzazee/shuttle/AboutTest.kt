// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.isToggleable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
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
        compose.setContent { AboutScreen("0.3.0", notices, null, {}) { back = true } }
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
        compose.setContent { AboutScreen("0.0.0-dev", null, null, {}) {} }
        compose.onNodeWithText("This build was made without them: a release carries them.").assertExists()
    }

    // The licenses' 78 columns run on; headings, rules, lists and short lines keep their own.
    @Test
    fun theNoticesReflow() {
        val license = """
            Redistribution and use in source and binary forms, with or without
            modification, are permitted provided that the following conditions are
            met:
               * Redistributions of source code must retain the above copyright
            notice, this list of conditions and the following disclaimer.
            Short line.
            Next.
        """.trimIndent()
        assertEquals(
            "Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:\n" +
                "* Redistributions of source code must retain the above copyright notice, this list of conditions and the following disclaimer. Short line.\nNext.",
            reflow(license),
        )
        val list = "- androidx.activity:activity-compose 1.13.0: Apache-2.0\n- androidx.work:work-runtime 2.12.0: Apache-2.0"
        assertEquals(list, reflow(list))
        val heading = "==============================================================================\n== Apache-2.0"
        assertEquals(heading, reflow(heading))
    }

    // The wallpaper's colours, where Android offers them: a switch that says what it does.
    @Test
    fun theWallpapersColours() {
        val set = mutableListOf<Boolean>()
        compose.setContent { AboutScreen("0.3.0", null, false, { set += it }) {} }
        compose.onNodeWithText("The wallpaper's colours").assertExists()
        compose.onNode(isToggleable()).assertIsOff().performClick()
        assertEquals(listOf(true), set)
    }

    @Test
    fun noWallpaperColoursToOffer() {
        compose.setContent { AboutScreen("0.3.0", null, null, {}) {} }
        compose.onNodeWithText("The wallpaper's colours").assertDoesNotExist()
    }
}

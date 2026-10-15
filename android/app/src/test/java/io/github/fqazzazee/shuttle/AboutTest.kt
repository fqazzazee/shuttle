// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.semantics.SemanticsProperties
import androidx.compose.ui.test.SemanticsMatcher
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
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
        compose.setContent { NoticesScreen(notices) { back = true } }
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
        compose.setContent { NoticesScreen(null) {} }
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

    private val released = "9dbba13a372fe3d1f3bba5f5ae41d7ab05fb0d425146467245d529f0d7797742"

    // A release: its checksums, said to be the release key's; the links.
    @Test
    fun aReleasedCopy() {
        var notices = 0
        compose.setContent { AboutScreen("0.2.0", ThisCopy("ab12", released), { notices++ }) {} }
        compose.onNodeWithText("Signed with shuttle's release key", substring = true).assertExists()
        compose.onNodeWithText("ab12").assertExists()
        compose.onNodeWithText(released).assertExists()
        compose.onNodeWithText("Version 0.2.0").assertExists()
        for (link in listOf("Source on GitHub", "Project page", "Releases", "Buy me a coffee", "License")) compose.onNodeWithText(link).assertExists()
        // The license is a link: its words are not on the page.
        compose.onNodeWithText("PolyForm", substring = true).assertDoesNotExist()
        compose.onNodeWithText("Third-party notices").performScrollTo().performClick()
        assertEquals(1, notices)
    }

    // A build of one's own, or one whose checksums cannot be read.
    @Test
    fun anotherCopy() {
        compose.setContent { AboutScreen("0.0.0-dev", ThisCopy(null, "ffff"), {}) {} }
        compose.onNodeWithText("Not signed with shuttle's release key", substring = true).assertExists()
        compose.onNodeWithText("cannot be read here").assertExists()
    }

    @Test
    fun whileTheChecksumsAreRead() {
        compose.setContent { AboutScreen("0.2.0", null, {}) {} }
        compose.onNodeWithText("Reading its checksums…").assertExists()
    }
}

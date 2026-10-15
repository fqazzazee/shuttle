// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.isToggleable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class AppTest {
    @get:Rule
    val compose = createComposeRule()

    // Its pages open from it; the wallpaper's colours switch, where Android offers them.
    @Test
    fun itsPagesAndTheWallpapersColours() {
        val done = mutableListOf<String>()
        compose.setContent { AppScreen(false, { done += "wallpaper $it" }, AppActions({ done += "help" }, { done += "backup" }, { done += "about" })) }
        compose.onNodeWithText("Help").performClick()
        compose.onNodeWithText("Backup and restore").performClick()
        compose.onNodeWithText("About shuttle").performClick()
        compose.onNodeWithText("The wallpaper's colours").assertExists()
        compose.onNode(isToggleable()).assertIsOff().performClick()
        assertEquals(listOf("help", "backup", "about", "wallpaper true"), done)
    }

    @Test
    fun noWallpaperColoursToOffer() {
        compose.setContent { AppScreen(null, {}, AppActions({}, {}, {})) }
        compose.onNodeWithText("The wallpaper's colours").assertDoesNotExist()
    }
}

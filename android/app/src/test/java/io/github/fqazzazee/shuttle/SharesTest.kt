// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class SharesTest {
    @get:Rule
    val compose = createComposeRule()

    // As the binding's SharesJSON writes it.
    private val json = """[
        {"Name":"card","Type":"path","Where":"/storage/1A2B-3C4D","UsedBy":["photos","music"],"State":"online",
         "Room":"12.0 GB free of 128.0 GB","Low":false},
        {"Name":"nas","Type":"smb","Where":"smb://user@nas.example/home","UsedBy":[],"State":"","Room":"","Low":false}]"""

    @Test
    fun parsesTheBindingsShares() {
        val rows = ShareRow.parseAll(json)
        assertEquals(listOf("card", "nas"), rows.map { it.name })
        assertEquals(listOf("photos", "music"), rows[0].usedBy)
    }

    @Test
    fun eachShareAsTheTuiListsIt() {
        compose.setContent { SharesScreen(ShareRow.parseAll(json), null, {}, {}) }
        for (text in listOf("card", "Online", "path · /storage/1A2B-3C4D", "Used by photos, music", "12.0 GB free of 128.0 GB",
            "nas", "Not looked at yet", "smb · smb://user@nas.example/home", "No shuttle uses it")) {
            compose.onNodeWithText(text).assertExists()
        }
    }

    @Test
    fun none() {
        compose.setContent { SharesScreen(emptyList(), null, {}, {}) }
        compose.onNodeWithText("No shares yet.").assertExists()
    }
}

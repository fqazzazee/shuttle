// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class BackupTest {
    @get:Rule
    val compose = createComposeRule()

    // As the binding's CheckBackup writes it; empty lists come as null.
    @Test
    fun parsesTheBindingsSummary() {
        val s = BackupSummary.parse("""{"Shares":["mnt","nas"],"Profiles":null,"Shuttles":["photos"],"NoPassword":["nas"],"NoFolder":null}""")
        assertEquals(BackupSummary(listOf("mnt", "nas"), emptyList(), listOf("photos"), listOf("nas"), emptyList()), s)
    }

    @Test
    fun saveAndRestore() {
        val done = mutableListOf<String>()
        compose.setContent {
            BackupScreen("Saved the backup.", false, null, BackupActions({ done += "save" }, { done += "restore" }, {}, {})) {}
        }
        compose.onNodeWithText("Passwords are not in it", substring = true).assertExists()
        compose.onNodeWithText("Save a backup…").performClick()
        compose.onNodeWithText("Restore a backup…").performClick()
        compose.onNodeWithText("Saved the backup.").assertExists()
        assertEquals(listOf("save", "restore"), done)
    }

    // Before it restores, what the backup brings, and what it lacks here.
    @Test
    fun aRestoreIsAskedAbout() {
        val done = mutableListOf<String>()
        val s = BackupSummary(listOf("nas"), listOf("live"), listOf("photos"), listOf("nas"), listOf("photos"))
        compose.setContent { BackupScreen(null, false, s, BackupActions({}, {}, { done += "yes" }, { done += "no" })) {} }
        compose.onNodeWithText("Restore this backup?").assertExists()
        compose.onNodeWithText("Shares: nas").assertExists()
        compose.onNodeWithText("Profiles: live").assertExists()
        compose.onNodeWithText("Passwords to set again on the Shares tab: nas").assertExists()
        compose.onNodeWithText("Folders not on this phone, for these shuttles: photos", substring = true).assertExists()
        compose.onNodeWithText("Cancel").performClick()
        compose.onNodeWithText("Restore").performClick()
        assertEquals(listOf("no", "yes"), done)
    }

    @Test
    fun nothingLackingNothingSaid() {
        val s = BackupSummary(listOf("nas"), emptyList(), emptyList(), emptyList(), emptyList())
        compose.setContent { BackupScreen(null, false, s, BackupActions({}, {}, {}, {})) {} }
        compose.onNodeWithText("Profiles: none").assertExists()
        compose.onNodeWithText("Passwords to set again", substring = true).assertDoesNotExist()
    }
}

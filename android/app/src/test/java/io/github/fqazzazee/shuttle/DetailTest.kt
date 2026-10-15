// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class DetailTest {
    @get:Rule
    val compose = createComposeRule()

    // As the binding's DetailJSON writes it.
    private val json = """{"Name":"photos","Mode":"push","Arrow":"↑","Profile":"phone","Local":"/storage/emulated/0/DCIM",
        "Remote":"nas:/Photos","LocalRoom":"12.0 GB free of 256.0 GB","RemoteRoom":"","LocalLow":false,"RemoteLow":false,
        "Syncs":"all but cache","Runs":"as files change","LastRun":"5m ago: 3 copied","Now":"","Waiting":"",
        "Attention":"1 conflict waiting for you",
        "Errors":[{"Path":"a.jpg","Op":3,"Reason":"permission denied","Outcome":"","Local":null,"Remote":null}],
        "Refused":[{"Path":"what?.txt","Op":0,"Reason":"the local folder cannot hold this name: it contains '?'"}]}"""

    @Test
    fun parsesTheBindingsDetail() {
        val d = Detail.parse(json)
        assertEquals("photos", d.name)
        assertEquals("nas:/Photos", d.remote)
        assertEquals(listOf(PathIssue("a.jpg", "permission denied")), d.errors)
        assertEquals("what?.txt", d.refused.single().path)
        assertTrue(Detail.parse(json.replace("\"Errors\":[", "\"Errors\":null,\"X\":[")).errors.isEmpty())
    }

    @Test
    fun showsItAllAndGoesBack() {
        var back = false
        var settings = false
        var edit = false
        compose.setContent { DetailScreen(Detail.parse(json), DetailActions({ back = true }, { edit = true }, { settings = true }, {}) { null }) }
        for (text in listOf("↑ photos", "push · profile phone", "/storage/emulated/0/DCIM\n12.0 GB free of 256.0 GB",
            "nas:/Photos", "all but cache", "as files change", "5m ago: 3 copied", "1 conflict waiting for you",
            "Files that could not be synced", "a.jpg", "permission denied", "what?.txt")) {
            compose.onNodeWithText(text).assertExists()
        }
        compose.onNodeWithText("Settings").performClick()
        assertTrue(settings)
        compose.onNodeWithText("Edit").performClick()
        assertTrue(edit)
        compose.onNodeWithText("← Back").performClick()
        assertTrue(back)
    }

    // Turned off and on, and deleted once asked; a refusal keeps the question open, saying why.
    @Test
    fun disabledEnabledAndDeleted() {
        val enabled = mutableListOf<Boolean>()
        var deletes = 0
        var detail by mutableStateOf(Detail.parse(json))
        compose.setContent {
            DetailScreen(detail, DetailActions({}, {}, {}, { enabled += it }) { if (deletes++ == 0) "the config file changed meanwhile" else null })
        }
        compose.onNodeWithText("Disable").performScrollTo().performClick()
        detail = detail.copy(enabled = false)
        compose.onNodeWithText("Disabled: it does not run, by itself or when asked, until it is enabled.").assertExists()
        compose.onNodeWithText("Enable").performScrollTo().performClick()
        assertEquals(listOf(false, true), enabled)
        compose.onNodeWithText("Delete…").performScrollTo().performClick()
        compose.onNodeWithText("Delete the shuttle photos from the config? Its files stay where they are, on both sides.").assertExists()
        compose.onNodeWithText("Delete").performClick()
        compose.onNodeWithText("the config file changed meanwhile").assertExists()
        compose.onNodeWithText("Delete").performClick()
        compose.onNodeWithText("Delete").assertDoesNotExist()
        assertEquals(2, deletes)
    }
}

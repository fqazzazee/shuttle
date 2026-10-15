// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextReplacement
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ShareEditorTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun aShareRoundTrips() {
        // As ShareJSON gives it (config.Share), back as SaveShare takes it.
        val form = ShareForm.of("""{"Name":"nas","Type":"smb","Host":"nas.example","Port":1445,"Share":"home","Export":"",
            "Path":"","User":"user","Domain":"WORKGROUP","MountPort":0}""")
        assertEquals("nas.example:1445", form.host)
        val j = JSONObject(form.json())
        assertEquals("nas.example", j.getString("Host"))
        assertEquals(1445, j.getInt("Port"))
        assertEquals("WORKGROUP", j.getString("Domain"))
        val nfs = ShareForm.of("""{"Name":"media","Type":"nfs3","Host":"nas","Export":"/export/media"}""")
        assertEquals("/export/media", nfs.share)
        assertEquals("/export/media", JSONObject(nfs.json()).getString("Export"))
        assertEquals("/storage/1A2B", JSONObject(ShareForm(type = "path", name = "card", path = "/storage/1A2B").json()).getString("Path"))
    }

    @Test
    fun whatIsMissing() {
        assertEquals(listOf(R.string.add_share_name, R.string.add_host, R.string.add_share, R.string.add_user), ShareForm().missing())
        assertEquals(listOf(R.string.share_path), ShareForm(type = "path", name = "card", path = "relative").missing())
    }

    private val noop = ShareActions({}, {}, {}, {})

    @Test
    fun aNewShareIsSaved() {
        var saved: ShareForm? = null
        compose.setContent { ShareEditor(ShareForm(), true, emptyList(), false, null, ShareActions({}, { saved = it }, {}, {})) }
        compose.onNode(hasText("Name for the share") and hasSetTextAction()).performTextReplacement("nas")
        compose.onNode(hasText("Server (name or address, :port if not the usual)") and hasSetTextAction()).performTextReplacement("nas.example")
        compose.onNode(hasText("Share on the server") and hasSetTextAction()).performTextReplacement("home")
        compose.onNode(hasText("User") and hasSetTextAction()).performTextReplacement("user")
        compose.onNodeWithText("Save").performScrollTo().performClick()
        assertEquals("nas.example", saved?.host)
    }

    @Test
    fun anEditKeepsItsNameAndOffersDeleteOnlyWhenUnused() {
        val nas = ShareForm(name = "nas", host = "nas.example", share = "home", user = "user")
        compose.setContent { ShareEditor(nas, false, listOf("photos"), false, null, noop) }
        compose.onNode(hasText("Name for the share")).assertIsNotEnabled()
        compose.onNodeWithText("Delete").assertDoesNotExist()
        compose.onNodeWithText("Used by photos").assertExists()
    }

    @Test
    fun deleteAsksFirst() {
        var deleted = ""
        val nas = ShareForm(name = "nas", host = "nas.example", share = "home", user = "user")
        compose.setContent { ShareEditor(nas, false, emptyList(), false, null, ShareActions({}, {}, { deleted = it }, {})) }
        compose.onNodeWithText("Delete").performScrollTo().performClick()
        compose.onNodeWithText("Delete the share nas from the config? Its password stays stored.").assertExists()
        compose.onAllNodesWithText("Delete").let { it[it.fetchSemanticsNodes().size - 1].performClick() }
        assertTrue(deleted == "nas")
    }
}

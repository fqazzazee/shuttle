// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.hasText
import androidx.compose.ui.test.junit4.createEmptyComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.compose.ui.test.performTextReplacement
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import org.json.JSONObject
import org.junit.Assume.assumeTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The phase's exit test: a shuttle set up through the app, as a user would, against a test SMB
 * server, and a run that copies a file to it. make test-android-flow runs it on a phone against
 * the Samba container (smbHost etc. as instrumentation arguments) and checks the share
 * afterwards. It works on the app's own config and a folder in shared storage, so without those
 * arguments, as in make test-android-device, it does not run.
 */
@RunWith(AndroidJUnit4::class)
class AppFlowTest {
    // Empty: the app is opened only once the test knows it runs.
    @get:Rule
    val compose = createEmptyComposeRule()

    private val args = InstrumentationRegistry.getArguments()

    private fun field(label: String, value: String) {
        compose.onNode(hasText(label) and hasSetTextAction()).performScrollTo().performTextReplacement(value)
    }

    private fun click(text: String) = compose.onNodeWithText(text).performScrollTo().performClick()

    private fun waitFor(what: String, millis: Long = 60_000, ok: () -> Boolean) {
        try {
            compose.waitUntil(millis, ok)
        } catch (e: Throwable) { // ComposeTimeoutException is a Throwable, not an Exception
            throw AssertionError("waiting for $what; statuses: ${Core.statuses}", e)
        }
    }

    @Test
    fun setUpAShuttleInTheAppAndSync() {
        val host = args.getString("smbHost")
        assumeTrue("needs smbHost, smbShare, smbUser, smbPassword: see make test-android-flow", host != null)
        val local = File("/storage/emulated/0/ShuttleCI").apply { mkdirs() }
        ActivityScenario.launch(MainActivity::class.java).use { flow(host!!, local) }
    }

    private fun flow(host: String, local: File) {
        if (compose.onAllNodesWithText("Continue").fetchSemanticsNodes().isNotEmpty()) click("Continue")
        click("Add a share and a shuttle")
        field("Server (name or address, :port if not the usual)", host)
        field("Share on the server", args.getString("smbShare")!!)
        field("User", args.getString("smbUser")!!)
        field("Password", args.getString("smbPassword")!!)
        click("Test the connection")
        waitFor("the connection test") {
            compose.onAllNodesWithText("Connected: the share is there.").fetchSemanticsNodes().isNotEmpty()
        }
        field("Name for the shuttle", "ci")
        field("Folder on the share (empty: all of it)", "android-ci")
        field("Folder on the phone", local.path)
        click("Push to the share")
        click("Save")

        waitFor("the new shuttle in the list") { compose.onAllNodesWithText("↑ ci").fetchSemanticsNodes().isNotEmpty() }
        File(local, "from-the-app.txt").writeText("set up in the app, and synced\n")
        compose.onAllNodesWithText("Sync now")[0].performScrollTo().performClick()
        waitFor("the run to copy the file") {
            Core.statuses["ci"]?.let { JSONObject(it).getInt("Copied") == 1 } == true
        }
    }
}

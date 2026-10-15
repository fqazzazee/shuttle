// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.ui.test.hasClickAction
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
 * The phase's exit test: a share and a shuttle set up through the app, as a user would (the Shares
 * tab, then the wizard), against a test SMB server, and a run that copies a file to it. make
 * test-android-flow runs it on a phone against the Samba container (smbHost etc. as
 * instrumentation arguments) and checks the share afterwards. It works on the app's own config and
 * a folder in shared storage, so without those arguments, as in make test-android-device, it does
 * not run.
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

    // In a dialog, which does not scroll.
    private fun dialogField(label: String, value: String) {
        compose.onNode(hasText(label) and hasSetTextAction()).performTextReplacement(value)
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
        // The share, as the Shares tab adds one.
        compose.onNode(hasText("Shares") and hasClickAction()).performClick()
        click("Add a share")
        field("Name for the share", "ci-share")
        field("Server (name or address, :port if not the usual)", host)
        field("Share on the server", args.getString("smbShare")!!)
        field("User", args.getString("smbUser")!!)
        field("Password", args.getString("smbPassword")!!)
        click("Test the connection")
        waitFor("the connection test") { shows("Connected: the share is there.") }
        click("Save")
        waitFor("the share saved") { shows("Saved the share ci-share.") }

        // The shuttle, through the wizard: its name and share, a new folder on the share, everything
        // in it, the folder on the phone, no profile, and push.
        compose.onNode(hasText("Shuttles") and hasClickAction()).performClick()
        click("Add a shuttle")
        field("The shuttle's name", "ci")
        click("ci-share")
        click("Next")
        waitFor("the share's top folder") { shows("New folder") }
        click("New folder")
        dialogField("Name", "android-ci")
        compose.onNodeWithText("Make").performClick()
        waitFor("the new folder on the share") { shows("ci-share:/android-ci") && shows("Nothing in this folder.") }
        click("Use this folder")
        click("Next")
        waitFor("the phone's storage") { shows("${local.name}/") }
        click("${local.name}/")
        waitFor("the folder on the phone") { shows(local.path) && shows("Nothing in this folder.") }
        click("Use this folder")
        click("No profile")
        click("Next")
        click("Mode")
        compose.onNodeWithText("push").performClick()
        compose.onNodeWithText("Save").performClick()
        click("Save the shuttle")

        waitFor("the new shuttle in the list") { shows("↑ ci") }
        File(local, "from-the-app.txt").writeText("set up in the app, and synced\n")
        compose.onAllNodesWithText("Sync now")[0].performScrollTo().performClick()
        waitFor("the run to copy the file") {
            Core.statuses["ci"]?.let { JSONObject(it).getInt("Copied") == 1 } == true
        }
    }

    private fun shows(text: String) = compose.onAllNodesWithText(text).fetchSemanticsNodes().isNotEmpty()
}

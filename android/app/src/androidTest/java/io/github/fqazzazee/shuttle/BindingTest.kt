// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.github.fqazzazee.shuttle.mobile.Mobile
import io.github.fqazzazee.shuttle.mobile.Options
import io.github.fqazzazee.shuttle.mobile.Passwords
import java.io.File
import org.json.JSONArray
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/**
 * The Go core through the .aar, on a phone or emulator: a share and a shuttle added as the add
 * screen adds them, and a run. Everything in a folder of the app's cache, removed after.
 */
@RunWith(AndroidJUnit4::class)
class BindingTest {
    private val dir = File(InstrumentationRegistry.getInstrumentation().targetContext.cacheDir, "binding-test")

    @After
    fun removeDir() {
        dir.deleteRecursively()
    }

    @Test
    fun addAShareAndAShuttleAndSync() {
        dir.deleteRecursively()
        val share = File(dir, "share").apply { mkdirs() }
        val local = File(dir, "local").apply { mkdirs() }
        val o = Options().apply {
            configPath = File(dir, "config.toml").path
            stateDir = File(dir, "state").path
            deviceName = "test phone"
            passwords = object : Passwords {
                override fun get(account: String) = ""
                override fun set(account: String, password: String) {}
                override fun delete(account: String) {}
            }
            filesAccess = true
            localNetwork = true
        }
        val d = Mobile.start(o)
        try {
            val form = AddForm(shareType = "path", shareName = "card", name = "t", local = local.path, mode = "push")
            d.addShare(JSONObject(form.shareJSON()).put("Path", share.path).toString(), "")
            d.addShuttle(form.shuttleJSON())
            // The daemon applies the edit in the background.
            val deadline = System.currentTimeMillis() + 10_000
            while (JSONArray(d.statusesJSON()).length() == 0) {
                assertTrue("the new shuttle never showed", System.currentTimeMillis() < deadline)
                Thread.sleep(50)
            }
            File(local, "a.txt").writeText("from the phone")
            val st = JSONObject(d.syncAndWait("t", 10_000))
            assertEquals("idle", st.getString("State"))
            assertEquals(1, st.getInt("Copied"))
            assertEquals("from the phone", File(share, "a.txt").readText())
        } finally {
            d.stop()
        }
    }
}

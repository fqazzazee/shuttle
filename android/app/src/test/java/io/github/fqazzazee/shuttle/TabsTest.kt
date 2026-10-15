// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

/** On the JVM with Robolectric: the tab bar, as a user taps it. */
@RunWith(AndroidJUnit4::class)
class TabsTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun eachTabOpensItsScreen() {
        var tab by mutableStateOf(Tab.SHUTTLES)
        compose.setContent { AppTabs(tab, { tab = it }) { t -> Text("screen of ${t.name}") } }
        compose.onNodeWithText("screen of SHUTTLES").assertExists()
        for ((label, t) in listOf("Conflicts" to Tab.CONFLICTS, "Activity" to Tab.ACTIVITY, "Profiles" to Tab.PROFILES,
            "Shares" to Tab.SHARES, "App" to Tab.APP, "Shuttles" to Tab.SHUTTLES)) {
            compose.onNodeWithText(label).performClick()
            compose.onNodeWithText("screen of ${t.name}").assertExists()
            assertEquals(t, tab)
        }
    }

    @Test
    fun aNoticeOpensItsTabByName() {
        assertEquals(Tab.CONFLICTS, Tab.of("conflicts"))
        assertEquals(Tab.SHARES, Tab.of("SHARES"))
        assertNull(Tab.of(""))
        assertNull(Tab.of(null))
        assertNull(Tab.of("something new"))
    }
}

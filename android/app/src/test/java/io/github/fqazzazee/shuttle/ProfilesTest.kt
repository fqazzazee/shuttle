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
class ProfilesTest {
    @get:Rule
    val compose = createComposeRule()

    // As the binding's ProfilesJSON writes it.
    private val json = """[{"Name":"phone","Mode":"push","Triggers":"realtime","UsedBy":["photos","music"]},
        {"Name":"quiet","Mode":"","Triggers":"manual only","UsedBy":[]}]"""

    @Test
    fun parsesTheBindingsProfiles() {
        assertEquals(listOf(ProfileRow("phone", "push", "realtime", listOf("photos", "music")), ProfileRow("quiet", "", "manual only", emptyList())),
            ProfileRow.parseAll(json))
    }

    @Test
    fun eachProfileAsTheTuiListsIt() {
        compose.setContent { ProfilesScreen(ProfileRow.parseAll(json)) }
        for (text in listOf("phone", "push · realtime", "Used by photos, music", "quiet", "— · manual only", "No shuttle uses it")) {
            compose.onNodeWithText(text).assertExists()
        }
    }

    @Test
    fun none() {
        compose.setContent { ProfilesScreen(emptyList()) }
        compose.onNodeWithText("No profiles yet. A profile is a set of settings that shuttles share.").assertExists()
    }
}

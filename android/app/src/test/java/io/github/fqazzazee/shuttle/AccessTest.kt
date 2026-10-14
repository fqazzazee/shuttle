// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import org.junit.Assert.assertEquals
import org.junit.Test

class AccessTest {
    @Test
    fun missingInTheFirstRunScreensOrder() {
        assertEquals(Permission.entries, Access(files = false, localNetwork = false, notifications = false).missing)
        assertEquals(listOf(Permission.LOCAL_NETWORK), Access(files = true, localNetwork = false, notifications = true).missing)
        assertEquals(emptyList<Permission>(), Access(files = true, localNetwork = true, notifications = true).missing)
    }

    @Test
    fun allFilesAccessIsOnlyInItsSettingsPage() {
        for (asked in listOf(false, true)) {
            for (rationale in listOf(false, true)) {
                assertEquals(Ask.ALL_FILES_SETTINGS, ask(Permission.FILES, asked, rationale))
            }
        }
    }

    @Test
    fun theDialogUntilAndroidStopsShowingIt() {
        for (p in listOf(Permission.LOCAL_NETWORK, Permission.NOTIFICATIONS)) {
            assertEquals("first time", Ask.DIALOG, ask(p, askedBefore = false, rationale = false))
            assertEquals("said no once", Ask.DIALOG, ask(p, askedBefore = true, rationale = true))
            assertEquals("said no for good", Ask.APP_SETTINGS, ask(p, askedBefore = true, rationale = false))
        }
    }
}

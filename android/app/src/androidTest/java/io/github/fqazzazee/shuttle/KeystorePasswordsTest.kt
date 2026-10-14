// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith

/** The password store under the real Keystore key, on a phone or emulator. */
@RunWith(AndroidJUnit4::class)
class KeystorePasswordsTest {
    private val context = InstrumentationRegistry.getInstrumentation().targetContext
    private val file = File(context.noBackupFilesDir, "passwords-test")

    @After
    fun removeFile() {
        file.delete()
    }

    @Test
    fun storesUnderTheKeystoreKey() {
        PasswordStore(file, KeystoreKey::get).set("smb://user@nas.example/home", "fake-pw")
        // The key is found again, not made anew: a second store opens the file.
        assertEquals("fake-pw", PasswordStore(file, KeystoreKey::get).get("smb://user@nas.example/home"))
        assertFalse(String(file.readBytes(), Charsets.ISO_8859_1).contains("fake-pw"))
    }

    @Test
    fun theKeyNeverLeavesTheKeystore() {
        assertNull(KeystoreKey.get().encoded)
    }
}

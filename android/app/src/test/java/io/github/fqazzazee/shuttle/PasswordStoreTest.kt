// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.io.File
import java.io.IOException
import java.nio.file.Files
import javax.crypto.AEADBadTagException
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

/** The store's format and its handling, under a software key in place of the Keystore's. */
class PasswordStoreTest {
    private val dir: File = Files.createTempDirectory("passwords").toFile().apply { deleteOnExit() }
    private val file = File(dir, "passwords")
    private val key = newKey()

    private fun newKey(): SecretKey = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()

    @Test
    fun storesGetsAndDeletes() {
        val store = PasswordStore(file) { key }
        assertEquals("", store.get("smb://user@nas.example/home"))
        assertFalse(file.exists())

        store.set("smb://user@nas.example/home", "fake-pw")
        store.set("smb://WORKGROUP;user@nas.example:1445/media", "tab\tnewline\nünï ✓")
        // Read back by another store over the same file, as after a restart.
        val again = PasswordStore(file) { key }
        assertEquals("fake-pw", again.get("smb://user@nas.example/home"))
        assertEquals("tab\tnewline\nünï ✓", again.get("smb://WORKGROUP;user@nas.example:1445/media"))

        again.set("smb://user@nas.example/home", "changed")
        again.delete("smb://WORKGROUP;user@nas.example:1445/media")
        again.delete("smb://nobody@nas.example/none")
        assertEquals("changed", store.get("smb://user@nas.example/home"))
        assertEquals("", store.get("smb://WORKGROUP;user@nas.example:1445/media"))
        assertEquals(listOf("passwords"), dir.list()!!.toList()) // no temp file left
    }

    @Test
    fun nothingInTheClear() {
        PasswordStore(file) { key }.set("smb://user@nas.example/home", "fake-pw")
        val bytes = String(file.readBytes(), Charsets.ISO_8859_1)
        assertTrue(bytes.startsWith("SHP1"))
        assertFalse(bytes.contains("fake-pw"))
        assertFalse(bytes.contains("nas.example"))
    }

    @Test
    fun aLostKeyIsAnErrorNotAnEmptyStore() {
        PasswordStore(file) { key }.set("smb://user@nas.example/home", "fake-pw")
        val other = PasswordStore(file) { newKey() }
        assertThrows(AEADBadTagException::class.java) { other.get("smb://user@nas.example/home") }
        // And a set under the wrong key does not wipe what is there.
        assertThrows(AEADBadTagException::class.java) { other.set("smb://user@nas.example/x", "y") }
        assertEquals("fake-pw", PasswordStore(file) { key }.get("smb://user@nas.example/home"))
    }

    @Test
    fun aDamagedFileIsAnError() {
        PasswordStore(file) { key }.set("smb://user@nas.example/home", "fake-pw")
        val bytes = file.readBytes()
        bytes[bytes.size - 1] = (bytes[bytes.size - 1].toInt() xor 1).toByte()
        file.writeBytes(bytes)
        assertThrows(AEADBadTagException::class.java) { PasswordStore(file) { key }.get("smb://user@nas.example/home") }

        file.writeText("not ours")
        assertThrows(IOException::class.java) { PasswordStore(file) { key }.get("smb://user@nas.example/home") }
    }

    @Test
    fun theFormatRoundTrips() {
        val all = mapOf("b" to "2", "a" to "", "ü" to "✓".repeat(1000))
        assertEquals(all, PasswordStore.decode(PasswordStore.encode(all)))
        assertEquals(emptyMap<String, String>(), PasswordStore.decode(PasswordStore.encode(emptyMap())))
    }
}

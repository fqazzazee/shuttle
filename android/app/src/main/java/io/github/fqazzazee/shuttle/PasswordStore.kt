// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import io.github.fqazzazee.shuttle.mobile.Passwords
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.File
import java.io.IOException
import javax.crypto.Cipher
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * The share passwords, as the daemon's password store (DECISIONS 2026-10-14, "Android: passwords
 * under the Keystore"): one file, sealed with AES-GCM under a key that, on the phone, never leaves
 * the Android Keystore. Each password is keyed by the server it opens, as the daemon names it
 * ("smb://user@nas.example/home").
 *
 * The daemon calls it from its own threads, so every method holds the store's lock. A file that
 * cannot be opened, under a lost key or damaged, is an error, never an empty store: the daemon
 * then says it cannot get the password, and nothing is forgotten silently.
 */
class PasswordStore(private val file: File, private val key: () -> SecretKey) : Passwords {
    @Synchronized
    override fun get(account: String): String = read()[account] ?: ""

    @Synchronized
    override fun set(account: String, password: String) {
        write(read() + (account to password))
    }

    @Synchronized
    override fun delete(account: String) {
        val all = read()
        if (account in all) write(all - account)
    }

    private fun read(): Map<String, String> {
        if (!file.exists()) return emptyMap()
        val bytes = file.readBytes()
        if (bytes.size < MAGIC.size + IV_SIZE || !bytes.copyOfRange(0, MAGIC.size).contentEquals(MAGIC)) {
            throw IOException("$file is not a shuttle password file")
        }
        val iv = bytes.copyOfRange(MAGIC.size, MAGIC.size + IV_SIZE)
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(TAG_BITS, iv))
        cipher.updateAAD(MAGIC)
        val plain = cipher.doFinal(bytes, MAGIC.size + IV_SIZE, bytes.size - MAGIC.size - IV_SIZE)
        return decode(plain)
    }

    /** Seals the store with a fresh IV and puts it in place in one rename. */
    private fun write(all: Map<String, String>) {
        // The Keystore makes the IV: it refuses one given for encryption.
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        cipher.updateAAD(MAGIC)
        val sealed = cipher.doFinal(encode(all))
        val tmp = File(file.parentFile, file.name + ".tmp")
        tmp.writeBytes(MAGIC + cipher.iv + sealed)
        if (!tmp.renameTo(file)) {
            tmp.delete()
            throw IOException("could not replace $file")
        }
    }

    companion object {
        /** Starts the file, and is authenticated with it: version 1 of the format. */
        private val MAGIC = "SHP1".toByteArray()
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private const val IV_SIZE = 12
        private const val TAG_BITS = 128

        internal fun encode(all: Map<String, String>): ByteArray {
            val out = ByteArrayOutputStream()
            DataOutputStream(out).use { d ->
                d.writeInt(all.size)
                for ((account, password) in all.toSortedMap()) {
                    d.writeUTF(account)
                    d.writeUTF(password)
                }
            }
            return out.toByteArray()
        }

        internal fun decode(bytes: ByteArray): Map<String, String> =
            DataInputStream(ByteArrayInputStream(bytes)).use { d ->
                buildMap { repeat(d.readInt()) { put(d.readUTF(), d.readUTF()) } }
            }
    }
}

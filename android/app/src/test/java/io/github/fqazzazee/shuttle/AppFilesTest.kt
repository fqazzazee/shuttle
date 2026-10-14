// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Test

class AppFilesTest {
    @Test
    fun everythingIsInPrivateStorage() {
        val app = "/data/user/0/io.github.fqazzazee.shuttle"
        val files = AppFiles(File("$app/files"), File("$app/no_backup"))
        assertEquals("$app/files/config.toml", files.config.path)
        assertEquals("$app/files/state", files.state.path)
        assertEquals("$app/no_backup/passwords", files.passwords.path)
    }
}

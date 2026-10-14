// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Test

class AppFilesTest {
    @Test
    fun everythingIsInPrivateStorage() {
        val files = AppFiles(File("/data/user/0/io.github.fqazzazee.shuttle/files"))
        assertEquals("/data/user/0/io.github.fqazzazee.shuttle/files/config.toml", files.config.path)
        assertEquals("/data/user/0/io.github.fqazzazee.shuttle/files/state", files.state.path)
    }
}

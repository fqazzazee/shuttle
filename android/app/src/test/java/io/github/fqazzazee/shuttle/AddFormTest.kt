// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AddFormTest {
    private val filled = AddForm(
        host = " nas.example ", share = "home", user = "user", password = "fake-pw",
        name = "photos", folder = "/Photos/", local = "/storage/emulated/0/DCIM/", mode = "push", runs = Runs.REAL_TIME,
    )

    @Test
    fun anSmbShareAsTheConfigHasIt() {
        val j = JSONObject(filled.shareJSON())
        assertEquals("nas", j.getString("Name"))
        assertEquals("smb", j.getString("Type"))
        assertEquals("nas.example", j.getString("Host"))
        assertEquals("home", j.getString("Share"))
        assertEquals("user", j.getString("User"))
        assertTrue("the password goes to the store, never the config", !j.has("Password"))
    }

    @Test
    fun aServerOnAnotherPort() {
        val j = JSONObject(filled.copy(host = "10.0.2.2:1445").shareJSON())
        assertEquals("10.0.2.2", j.getString("Host"))
        assertEquals(1445, j.getInt("Port"))
        assertTrue("no port given: the protocol's", !JSONObject(filled.shareJSON()).has("Port"))
        // An IPv6 address is not a host and a port.
        assertEquals("fe80::1", JSONObject(filled.copy(host = "fe80::1").shareJSON()).getString("Host"))
    }

    @Test
    fun anNfsShareByItsExport() {
        val j = JSONObject(filled.copy(shareType = "nfs3", share = "/export/media").shareJSON())
        assertEquals("nfs3", j.getString("Type"))
        assertEquals("/export/media", j.getString("Export"))
        assertTrue(!j.has("Share") && !j.has("User"))
    }

    @Test
    fun theShuttle() {
        val j = JSONObject(filled.shuttleJSON())
        assertEquals("photos", j.getString("Name"))
        assertEquals("nas", j.getString("Share"))
        assertEquals("Photos", j.getString("Folder"))
        assertEquals("/storage/emulated/0/DCIM", j.getString("Local"))
        assertEquals("push", j.getString("Mode"))
        assertEquals("realtime", j.getJSONArray("Triggers").getString(0))
        assertEquals("1h", JSONObject(filled.copy(runs = Runs.HOURLY).shuttleJSON()).getString("Interval"))
        assertEquals(0, JSONObject(filled.copy(runs = Runs.ASKED).shuttleJSON()).getJSONArray("Triggers").length())
    }

    @Test
    fun whatIsMissing() {
        assertEquals(emptyList<Int>(), filled.missing())
        assertEquals(
            listOf(R.string.add_host, R.string.add_share, R.string.add_user, R.string.add_name, R.string.add_local),
            AddForm().missing(),
        )
        // A folder in the phone's storage, never all of it.
        for (root in listOf("/storage/emulated/0/", "/sdcard", "relative/path")) {
            assertEquals(listOf(R.string.add_local), filled.copy(local = root).missing())
        }
        // NFS asks for no user, and calls its share an export.
        assertEquals(listOf(R.string.add_export), filled.copy(shareType = "nfs3", user = "", share = "").missing())
    }
}

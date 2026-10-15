// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import org.json.JSONArray
import org.json.JSONObject

/** How a new shuttle runs: when asked, as files change, or on a schedule. */
enum class Runs(val triggers: List<String>, val interval: String) {
    ASKED(emptyList(), ""),
    REAL_TIME(listOf("realtime"), ""),
    HOURLY(listOf("schedule"), "1h"),
}

/**
 * A first share and shuttle, as the minimal form has them (group C's editor replaces it): what
 * the binding's AddShare and AddShuttle take, and what is still missing.
 */
data class AddForm(
    val shareType: String = "smb",
    val shareName: String = "nas",
    val host: String = "",
    val share: String = "",
    val user: String = "",
    val password: String = "",
    val name: String = "",
    val folder: String = "",
    val local: String = "/storage/emulated/0/",
    val mode: String = "sync",
    val runs: Runs = Runs.ASKED,
) {
    val smb get() = shareType == "smb"

    /** The share, as the config has it: an SMB share by name, an NFS one by export. */
    fun shareJSON(): String = JSONObject().apply {
        put("Name", shareName.trim())
        put("Type", shareType)
        put("Host", host.trim())
        if (smb) {
            put("Share", share.trim())
            put("User", user.trim())
        } else {
            put("Export", share.trim())
        }
    }.toString()

    fun shuttleJSON(): String = JSONObject().apply {
        put("Name", name.trim())
        put("Share", shareName.trim())
        put("Folder", folder.trim().trim('/'))
        put("Local", local.trim().trimEnd('/'))
        put("Mode", mode)
        put("Triggers", JSONArray(runs.triggers))
        put("Interval", runs.interval)
    }.toString()

    /** The fields still empty that must not be, by their labels' string ids. */
    fun missing(): List<Int> = buildList {
        if (shareName.isBlank()) add(R.string.add_share_name)
        if (host.isBlank()) add(R.string.add_host)
        if (share.isBlank()) add(if (smb) R.string.add_share else R.string.add_export)
        if (smb && user.isBlank()) add(R.string.add_user)
        if (name.isBlank()) add(R.string.add_name)
        val l = local.trim().trimEnd('/')
        if (!l.startsWith("/") || l in STORAGE_ROOTS) add(R.string.add_local)
    }

    private companion object {
        /** A folder in the phone's storage, not all of it: never a whole card or the whole phone. */
        @Suppress("SdCardPath") // paths refused as a whole folder to sync, never written to
        val STORAGE_ROOTS = setOf("", "/storage", "/storage/emulated", "/storage/emulated/0", "/sdcard", "/mnt/sdcard")
    }
}

// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.Context
import android.content.pm.PackageManager
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import java.io.File
import java.io.IOException
import java.security.MessageDigest

/**
 * What the About screen says of this copy of the app, to check it against the release it came
 * from: the SHA-256 of its APK, as the release's checksums.txt lists it, and of the certificate it
 * is signed with. Either is null when it cannot be read.
 */
data class ThisCopy(val apkSha256: String?, val certSha256: String?) {
    companion object {
        /** Reads both: the APK is read whole, so not on the main thread. */
        fun of(context: Context): ThisCopy = ThisCopy(apkSha256(context), certSha256(context))

        private fun apkSha256(context: Context): String? = try {
            // An APK installed from a release is one file; one installed in parts has no single checksum.
            val info = context.applicationInfo
            if (!info.splitSourceDirs.isNullOrEmpty()) {
                null
            } else {
                val digest = MessageDigest.getInstance("SHA-256")
                File(info.sourceDir).inputStream().use { input ->
                    val buffer = ByteArray(1 shl 16)
                    while (true) {
                        val n = input.read(buffer)
                        if (n < 0) break
                        digest.update(buffer, 0, n)
                    }
                }
                digest.digest().toHex()
            }
        } catch (_: IOException) {
            null
        }

        private fun certSha256(context: Context): String? = try {
            val info = context.packageManager.getPackageInfo(context.packageName, PackageManager.GET_SIGNING_CERTIFICATES)
            info.signingInfo?.apkContentsSigners?.singleOrNull()?.let { MessageDigest.getInstance("SHA-256").digest(it.toByteArray()).toHex() }
        } catch (_: PackageManager.NameNotFoundException) {
            null
        }

        private fun ByteArray.toHex() = joinToString("") { "%02x".format(it) }
    }
}

/** Where the app's links go. */
private val links = listOf(
    R.string.about_github to "https://github.com/fqazzazee/shuttle",
    R.string.about_page to "https://fqazzazee.github.io/shuttle/",
    R.string.about_releases to "https://github.com/fqazzazee/shuttle/releases",
    R.string.about_coffee to "https://buymeacoffee.com/fqazzazee",
)

/** The notices the build puts among the APK's assets (make android-notices). */
const val NOTICES_ASSET = "THIRD_PARTY_NOTICES"

/** The notices in paragraphs, as the About screen shows them; null in a build without them. */
fun readNotices(context: Context): List<String>? = try {
    context.assets.open(NOTICES_ASSET).bufferedReader().use { it.readText() }.split(Regex("\n{2,}")).filter { it.isNotBlank() }.map(::reflow)
} catch (_: IOException) {
    null
}

/**
 * This copy's checksums, to compare with the release's: its APK's with checksums.txt, its
 * certificate's with the release key's, and whether that is the release key, which every release
 * is signed with, so that each installs over the last.
 */
@Composable
private fun ThisCopyCard(copy: ThisCopy?) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(stringResource(R.string.about_this_copy), style = MaterialTheme.typography.titleMedium)
            if (copy == null) {
                Text(stringResource(R.string.about_reading), style = MaterialTheme.typography.bodyMedium)
                return@Column
            }
            val released = copy.certSha256 == stringResource(R.string.release_cert_sha256)
            Text(
                stringResource(if (released) R.string.about_release_key else R.string.about_not_release_key),
                style = MaterialTheme.typography.bodyMedium,
                color = if (released) MaterialTheme.colorScheme.secondary else MaterialTheme.colorScheme.tertiary,
            )
            SelectionContainer {
                Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    Checksum(R.string.about_apk_sha256, copy.apkSha256 ?: stringResource(R.string.about_unreadable))
                    Checksum(R.string.about_cert_sha256, copy.certSha256 ?: stringResource(R.string.about_unreadable))
                }
            }
        }
    }
}

@Composable
private fun Checksum(label: Int, value: String) {
    Column {
        Text(stringResource(label), style = MaterialTheme.typography.labelLarge)
        Text(value, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
    }
}

/**
 * A paragraph of the notices as the screen shows it. The licenses are wrapped at 78 columns, for a
 * terminal, and the phone wraps them again: a long line runs on into the next instead. A line keeps
 * its own after a short one or one ending in ":", and for a heading, a rule or a list item ("- ").
 */
fun reflow(paragraph: String): String {
    val out = StringBuilder()
    var prev: String? = null
    for (line in paragraph.lines().map(String::trim)) {
        val own = prev == null || prev.length < REFLOW_WIDTH || prev.endsWith(":") || prev.startsWith("==") ||
            line.startsWith("- ") || line.startsWith("-- ") || line.startsWith("==")
        if (prev != null) out.append(if (own) '\n' else ' ')
        out.append(line)
        prev = line
    }
    return out.toString()
}

/** Shorter than this, a line of the notices ends where it does: a title, a sentence's end. */
private const val REFLOW_WIDTH = 50

/**
 * About the app: its version, its links (the source on GitHub, the project page, the releases, Buy
 * me a coffee), this copy's checksums against the release's, its license, and the third-party
 * notices of what it includes, as the release carries them. copy is null while it is read.
 * wallpaper is whether the app takes the wallpaper's colours in place of shuttle's palette; null
 * where Android has none to offer.
 */
@Composable
fun AboutScreen(
    version: String,
    copy: ThisCopy?,
    notices: List<String>?,
    wallpaper: Boolean?,
    onWallpaper: (Boolean) -> Unit,
    onBack: () -> Unit,
) {
    val uri = LocalUriHandler.current
    // Lazily: the notices are a few thousand lines.
    LazyColumn(
        Modifier.safeDrawingPadding().padding(horizontal = 24.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item { TextButton(onClick = onBack) { Text(stringResource(R.string.back)) } }
        item { ShuttleLogo(Modifier.fillMaxWidth().padding(vertical = 8.dp)) }
        item { Text(stringResource(R.string.about_version, version), style = MaterialTheme.typography.bodyLarge) }
        item {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for ((label, url) in links) OutlinedButton(onClick = { uri.openUri(url) }) { Text(stringResource(label)) }
            }
        }
        item { ThisCopyCard(copy) }
        item { Text(stringResource(R.string.about_license), style = MaterialTheme.typography.bodyMedium) }
        wallpaper?.let { on ->
            item {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(stringResource(R.string.about_wallpaper), style = MaterialTheme.typography.titleSmall)
                        Text(stringResource(R.string.about_wallpaper_hint), style = MaterialTheme.typography.bodySmall)
                    }
                    Switch(on, onWallpaper)
                }
            }
        }
        item { Text(stringResource(R.string.about_notices), style = MaterialTheme.typography.titleMedium) }
        if (notices == null) {
            item { Text(stringResource(R.string.about_no_notices), style = MaterialTheme.typography.bodyMedium) }
        } else {
            items(notices) { Text(it, style = MaterialTheme.typography.bodySmall) }
        }
        item { Text("", Modifier.padding(bottom = 24.dp)) }
    }
}

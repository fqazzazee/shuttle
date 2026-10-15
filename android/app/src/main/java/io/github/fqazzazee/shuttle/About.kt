// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import java.io.IOException

/** The notices the build puts among the APK's assets (make android-notices). */
const val NOTICES_ASSET = "THIRD_PARTY_NOTICES"

/** The notices in paragraphs, as the About screen shows them; null in a build without them. */
fun readNotices(context: Context): List<String>? = try {
    context.assets.open(NOTICES_ASSET).bufferedReader().use { it.readText() }.split(Regex("\n{2,}")).filter { it.isNotBlank() }.map(::reflow)
} catch (_: IOException) {
    null
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
 * About the app: its version, its license, and the third-party notices of what it includes, the Go
 * core's modules and the Java and Kotlin libraries, as the release carries them.
 */
@Composable
fun AboutScreen(version: String, notices: List<String>?, onBack: () -> Unit) {
    // Lazily: the notices are a few thousand lines.
    LazyColumn(
        Modifier.safeDrawingPadding().padding(horizontal = 24.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item { TextButton(onClick = onBack) { Text(stringResource(R.string.back)) } }
        item { Text(stringResource(R.string.app_name), style = MaterialTheme.typography.headlineMedium) }
        item { Text(stringResource(R.string.about_version, version), style = MaterialTheme.typography.bodyLarge) }
        item { Text(stringResource(R.string.about_license), style = MaterialTheme.typography.bodyMedium) }
        item { Text(stringResource(R.string.about_notices), style = MaterialTheme.typography.titleMedium) }
        if (notices == null) {
            item { Text(stringResource(R.string.about_no_notices), style = MaterialTheme.typography.bodyMedium) }
        } else {
            items(notices) { Text(it, style = MaterialTheme.typography.bodySmall) }
        }
        item { Text("", Modifier.padding(bottom = 24.dp)) }
    }
}

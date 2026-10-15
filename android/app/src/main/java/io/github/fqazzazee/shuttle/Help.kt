// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp

/** The Help screen's questions and their answers, in the order a new user meets them. */
val helpTopics = listOf(
    R.string.help_start_q to R.string.help_start_a,
    R.string.help_words_q to R.string.help_words_a,
    R.string.help_modes_q to R.string.help_modes_a,
    R.string.help_runs_q to R.string.help_runs_a,
    R.string.help_what_q to R.string.help_what_a,
    R.string.help_waiting_q to R.string.help_waiting_a,
    R.string.help_conflicts_q to R.string.help_conflicts_a,
    R.string.help_deletions_q to R.string.help_deletions_a,
    R.string.help_versions_q to R.string.help_versions_a,
    R.string.help_left_out_q to R.string.help_left_out_a,
    R.string.help_passwords_q to R.string.help_passwords_a,
    R.string.help_stopped_q to R.string.help_stopped_a,
    R.string.help_desktop_q to R.string.help_desktop_a,
)

/** How to use the app, as questions; each opens to its answer, and closes again. */
@Composable
fun HelpScreen(onBack: () -> Unit) {
    var open by rememberSaveable { mutableStateOf<Int?>(null) }
    LazyColumn(
        Modifier.safeDrawingPadding().padding(horizontal = 24.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        item { TextButton(onClick = onBack) { Text(stringResource(R.string.back)) } }
        item { Text(stringResource(R.string.help), style = MaterialTheme.typography.headlineMedium) }
        items(helpTopics) { (question, answer) ->
            Column(
                Modifier.fillMaxWidth().clickable { open = if (open == question) null else question }.padding(vertical = 12.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Text(stringResource(question), style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.secondary)
                if (open == question) Text(stringResource(answer), style = MaterialTheme.typography.bodyMedium)
            }
            HorizontalDivider()
        }
        item { Text("", Modifier.padding(bottom = 24.dp)) }
    }
}

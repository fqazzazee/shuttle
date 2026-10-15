// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.core.content.edit
import androidx.core.net.toUri
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject

class MainActivity : ComponentActivity() {
    private var access by mutableStateOf(Access(files = false, localNetwork = false, notifications = false))
    private var firstRunSeen by mutableStateOf(false)
    private var forceStopped by mutableStateOf(false)
    private val pauses by lazy { Core.pauses(this) }
    private var pausedAll by mutableStateOf(false)
    private var adding by mutableStateOf(false)
    private var tab by mutableStateOf(Tab.SHUTTLES)
    private var selected by mutableStateOf<String?>(null)
    private var settingsOf by mutableStateOf<String?>(null)
    // Bumped after a setting is saved: the rows are read from the config file again.
    private var settingsRead by mutableStateOf(0)
    private var resolveError by mutableStateOf<String?>(null)
    private var activityShuttle by mutableStateOf("")
    private var problemsOnly by mutableStateOf(false)
    private var shareEditing by mutableStateOf<Pair<ShareForm, Boolean>?>(null) // the form, and whether it is new
    private var shareBusy by mutableStateOf(false)
    private var shareMessage by mutableStateOf<String?>(null)
    private var sharesNote by mutableStateOf<String?>(null)
    private var wizard by mutableStateOf<WizardState?>(null)
    // Bumped by Try again: the folder is read again.
    private var browseTry by mutableStateOf(0)
    private var profileOf by mutableStateOf<String?>(null)
    // Bumped after a profile is changed: its rows, and the list, are read from the config file again.
    private var profileRead by mutableStateOf(0)
    private var profilesNote by mutableStateOf<String?>(null)
    private var newProfile by mutableStateOf(false)
    private val prefs by lazy { getSharedPreferences("app", MODE_PRIVATE) }

    private val request = registerForActivityResult(ActivityResultContracts.RequestPermission()) {
        access = Access.of(this)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        openTab(intent)
        firstRunSeen = prefs.getBoolean(FIRST_RUN_SEEN, false)
        pausedAll = pauses.all
        // Opening the app after a force stop starts syncing again (the screen holds the daemon,
        // which starts the service and plans the jobs); say that it had stopped.
        val lastSeen = prefs.getLong(LAST_EXIT_SEEN, 0)
        val exits = recentExits(this)
        forceStopped = forceStoppedSince(exits, lastSeen)
        if (forceStopped) Log.i("shuttle", "force stop noticed; syncing starts again")
        prefs.edit { putLong(LAST_EXIT_SEEN, newestExit(exits, lastSeen)) }
        setContent {
            ShuttleTheme {
                Surface(Modifier.fillMaxSize()) {
                    if (!firstRunSeen) {
                        PermissionsScreen(access, ::allow) {
                            prefs.edit { putBoolean(FIRST_RUN_SEEN, true) }
                            firstRunSeen = true
                        }
                    } else if (adding) {
                        AddScreen { adding = false }
                    } else {
                        AppTabs(tab, { tab = it }) { t ->
                            when (t) {
                                Tab.SHUTTLES -> {
                                    // Read again whenever its status comes, so the panel follows a run;
                                    // a shuttle gone from the config shows the list again.
                                    val json = selected?.let { name ->
                                        Core.statuses[name]?.let { runCatching { Core.current?.detailJSON(name) }.getOrNull() }
                                    }
                                    val rows = settingsOf?.let { name ->
                                        settingsRead.let { runCatching { Core.current?.settingsJSON(name) }.getOrNull() }
                                    }
                                    val w = wizard
                                    if (w != null) {
                                        BackHandler { wizardBack() }
                                        Wizard(w)
                                    } else if (rows != null) {
                                        BackHandler { settingsOf = null }
                                        SettingsScreen(
                                            stringResource(R.string.settings_title, settingsOf!!), Setting.parseAll(rows), ::setSetting,
                                        ) { settingsOf = null }
                                    } else if (json != null) {
                                        // Back to the list, by the button or Android's back gesture.
                                        BackHandler { selected = null }
                                        DetailScreen(Detail.parse(json), { selected = null }) { settingsOf = selected }
                                    } else {
                                        Main(
                                            access, Core.statuses, Core.startError, forceStopped, { forceStopped = false },
                                            ::allow, pausedAll, actions, { wizard = WizardState() },
                                        ) { adding = true }
                                    }
                                }
                                Tab.CONFLICTS -> ConflictsScreen(
                                    ConflictItem.held(Core.statuses), HeldDeletions.of(Core.statuses), resolveError, ::resolve,
                                    ::allowDeletions,
                                )
                                Tab.ACTIVITY -> {
                                    // Read again when a run ends (a LastRun changes), not on every
                                    // status: a run sends its progress twice a second.
                                    val runs = Core.statuses.map { (n, j) -> n + JSONObject(j).optString("LastRun") }.sorted()
                                    val entries by produceState<List<Entry>?>(null, activityShuttle, runs) {
                                        value = withContext(Dispatchers.IO) {
                                            runCatching { Core.current?.activityJSON(activityShuttle, ACTIVITY_SHOWN) }
                                                .getOrNull()?.let(Entry::parseAll) ?: emptyList()
                                        }
                                    }
                                    ActivityScreen(
                                        Core.statuses.keys.sorted(), entries, activityShuttle, { activityShuttle = it },
                                        problemsOnly, { problemsOnly = it },
                                    )
                                }
                                Tab.SHARES -> {
                                    // Read again with every status: a share's state and room come from its shuttles.
                                    Core.statuses
                                    val rows = runCatching { Core.current?.sharesJSON() }.getOrNull()?.let(ShareRow::parseAll) ?: emptyList()
                                    val editing = shareEditing
                                    if (editing != null) {
                                        BackHandler { shareEditing = null }
                                        val (form, isNew) = editing
                                        ShareEditor(
                                            form, isNew, rows.firstOrNull { it.name == form.name }?.usedBy ?: emptyList(),
                                            shareBusy, shareMessage, shareActions,
                                        )
                                    } else {
                                        SharesScreen(rows, sharesNote, ::openShare) {
                                            shareMessage = null
                                            shareEditing = ShareForm() to true
                                        }
                                    }
                                }
                                Tab.PROFILES -> {
                                    // A reload brings statuses: the config file may have changed.
                                    Core.statuses
                                    val profiles = profileRead.let { runCatching { Core.current?.profilesJSON() }.getOrNull() }
                                        ?.let(ProfileRow::parseAll) ?: emptyList()
                                    val rows = profileOf?.let { name ->
                                        profileRead.let { runCatching { Core.current?.profileSettingsJSON(name) }.getOrNull() }
                                    }
                                    if (rows != null) {
                                        BackHandler { profileOf = null }
                                        val name = profileOf!!
                                        ProfileEditor(
                                            name, Setting.parseAll(rows), profiles.firstOrNull { it.name == name }?.usedBy ?: emptyList(),
                                            ::setProfileSetting, profileActions,
                                        ) { profileOf = null }
                                    } else {
                                        ProfilesScreen(profiles, profilesNote, { profilesNote = null; profileOf = it }) { newProfile = true }
                                        if (newProfile) NewProfileDialog(profileActions.create) { newProfile = false }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    // A notice's tap opens the app on its tab, whether the app was open or not.
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        openTab(intent)
    }

    private fun openTab(intent: Intent?) {
        Tab.of(intent?.getStringExtra(Notices.EXTRA_TAB))?.let { tab = it }
    }

    // The screen holds the daemon while it is shown, and starts the foreground service if a
    // shuttle is real-time.
    override fun onStart() {
        super.onStart()
        Core.async { Core.acquire(this, SCREEN)?.let { Core.updateService(this, it) } }
    }

    override fun onStop() {
        Core.async { Core.release(SCREEN) }
        super.onStop()
    }

    // Permissions are given in Settings and in dialogs, and can be taken back there at any
    // time: look again whenever the app comes back, and tell the daemon.
    override fun onResume() {
        super.onResume()
        access = Access.of(this)
        val now = access
        Core.async { Core.setAccess(now) }
    }

    private val actions = ListActions(
        open = { selected = it },
        syncNow = { SyncService.syncNow(this, it) },
        setPaused = { name, paused ->
            pauses.set(name, paused)
            Core.async { Core.current?.setPaused(name, paused) }
        },
        setPausedAll = { paused ->
            pauses.setAll(paused)
            pausedAll = paused
            Core.async { Core.current?.setPaused("", paused) }
        },
    )

    private fun openShare(name: String) {
        val json = runCatching { Core.current?.shareJSON(name) }.getOrNull() ?: return
        shareMessage = null
        shareEditing = ShareForm.of(json) to false
    }

    /** Runs a share editor's action off the main thread: a test or a save reaches the server or the file. */
    private fun shareTask(f: () -> String?) {
        shareBusy = true
        shareMessage = null
        Thread {
            shareMessage = try {
                f()
            } catch (e: Exception) {
                e.message
            }
            shareBusy = false
        }.start()
    }

    private val shareActions = ShareActions(
        test = { form ->
            shareTask {
                Core.current?.testShare(form.json(), form.password)
                getString(R.string.add_tested)
            }
        },
        save = { form ->
            val isNew = shareEditing?.second ?: true
            shareTask {
                val none = Core.current?.saveShare(form.json(), form.password, isNew) ?: return@shareTask getString(R.string.add_no_daemon)
                sharesNote = getString(if (none) R.string.share_saved_no_password else R.string.share_saved, form.name.trim())
                shareEditing = null
                null
            }
        },
        delete = { name ->
            shareTask {
                Core.current?.deleteShare(name)
                sharesNote = getString(R.string.share_deleted, name)
                shareEditing = null
                null
            }
        },
        cancel = { shareEditing = null },
    )

    /** Settles a conflict, off the main thread: the daemon waits for a run in progress to finish its file. */
    private fun resolve(c: ConflictItem, keep: Keep) {
        resolveError = null
        Thread {
            try {
                Core.current?.resolve(c.shuttle, c.path, keep.choice)
            } catch (e: Exception) {
                resolveError = e.message
            }
        }.start()
    }

    /** Lets the shuttle's next run make the deletions the guard held back; it starts that run. */
    private fun allowDeletions(shuttle: String) {
        resolveError = null
        Thread {
            try {
                Core.current?.allowDeletions(shuttle)
            } catch (e: Exception) {
                resolveError = e.message
            }
        }.start()
    }

    /** Sets one of the open shuttle's settings: null, or why it was refused. */
    private fun setSetting(key: String, text: String): String? {
        val name = settingsOf ?: return null
        return try {
            Core.current?.setSetting(name, key, text)
            settingsRead++
            null
        } catch (e: Exception) {
            e.message
        }
    }

    @Composable
    private fun Wizard(w: WizardState) {
        when (w.step) {
            WizardStep.SHARE -> {
                val shares = runCatching { Core.current?.sharesJSON() }.getOrNull()?.let(ShareRow::parseAll) ?: emptyList()
                WizardShareStep(shares, w.share, { wizard = w.copy(share = it, dir = "/") }, { wizard = w.copy(step = WizardStep.FOLDER) }, ::closeWizard)
            }
            WizardStep.FOLDER -> {
                // Read off the main thread: the share is reached over the network.
                val listing by produceState<Listing?>(null, w.share, w.dir, browseTry) {
                    value = null
                    value = withContext(Dispatchers.IO) {
                        try {
                            val json = Core.current?.browseJSON(w.share, w.dir)
                            if (json == null) Listing(null, getString(R.string.add_no_daemon)) else Listing(FolderEntry.parseAll(json), null)
                        } catch (e: Exception) {
                            Listing(null, e.message)
                        }
                    }
                }
                WizardFolderStep(w, listing, { wizard = it }, { browseTry++ }, ::wizardBack) { wizard = w.copy(step = WizardStep.LATER) }
            }
            WizardStep.LATER -> WizardLater(w.remote, ::closeWizard)
        }
    }

    private fun wizardBack() {
        val w = wizard ?: return
        when (w.step) {
            WizardStep.SHARE -> closeWizard()
            WizardStep.FOLDER -> wizard = w.copy(step = WizardStep.SHARE)
            WizardStep.LATER -> wizard = w.copy(step = WizardStep.FOLDER)
        }
    }

    // The share stays open between listings; let it go off the main thread, as a listing in
    // progress holds it.
    private fun closeWizard() {
        wizard = null
        Thread { Core.current?.endBrowse() }.start()
    }

    /** Sets one of the open profile's settings: null, or why it was refused. */
    private fun setProfileSetting(key: String, text: String): String? {
        val name = profileOf ?: return null
        return refusal {
            Core.current?.setProfileSetting(name, key, text)
            profileRead++
        }
    }

    // Each opens what it made, or goes back to the list after a delete.
    private val profileActions = ProfileActions(
        create = { name, mode ->
            refusal {
                Core.current?.newProfile(name, mode)
                profileOf = name
                profileRead++
            }
        },
        duplicate = { from, to ->
            refusal {
                Core.current?.duplicateProfile(from, to)
                profileOf = to
                profileRead++
            }
        },
        delete = { name ->
            refusal {
                Core.current?.deleteProfile(name)
                profilesNote = getString(R.string.profile_deleted, name)
                profileOf = null
                profileRead++
            }
        },
    )

    /** Runs f: null, or why it failed. */
    private fun refusal(f: () -> Unit): String? = try {
        f()
        null
    } catch (e: Exception) {
        e.message
    }

    private fun allow(p: Permission) {
        val name = p.system
        val asked = "asked:" + p.name
        val how = ask(p, prefs.getBoolean(asked, false), name != null && shouldShowRequestPermissionRationale(name))
        val app = "package:$packageName".toUri()
        when (how) {
            Ask.ALL_FILES_SETTINGS -> startActivity(Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION, app))
            Ask.APP_SETTINGS -> startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, app))
            Ask.DIALOG -> {
                prefs.edit { putBoolean(asked, true) }
                name?.let { request.launch(it) }
            }
        }
    }

    private companion object {
        const val FIRST_RUN_SEEN = "first_run_seen"
        const val SCREEN = "screen"
        const val LAST_EXIT_SEEN = "last_exit_seen"

        /** How many of the newest entries the Activity tab reads, as the TUI's. */
        const val ACTIVITY_SHOWN = 500L
    }
}

/** The app's main screen: what needs saying first, then the shuttles. */
@Composable
private fun Main(
    access: Access,
    statuses: Map<String, String>,
    startError: String?,
    forceStopped: Boolean,
    onSeen: () -> Unit,
    onAllow: (Permission) -> Unit,
    pausedAll: Boolean,
    actions: ListActions,
    onWizard: () -> Unit,
    onAdd: () -> Unit,
) {
    Column(
        Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.app_name), style = MaterialTheme.typography.headlineMedium)
        if (forceStopped && statuses.isNotEmpty()) ForceStopped(onSeen)
        MissingPermissions(access, onAllow)
        startError?.let { Text(stringResource(R.string.start_failed, it), color = MaterialTheme.colorScheme.error) }
        if (statuses.isEmpty()) {
            Text(stringResource(R.string.not_set_up))
        } else {
            ShuttleList(statuses.toSortedMap().values.map(ShuttleStatus::parse), pausedAll, actions)
        }
        OutlinedButton(onClick = onWizard, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.wizard_add)) }
        OutlinedButton(onClick = onAdd, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.add)) }
    }
}

/** After a force stop: nothing synced while the app was stopped, and it syncs again now. */
@Composable
private fun ForceStopped(onSeen: () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.force_stopped_title), style = MaterialTheme.typography.titleMedium)
            Text(stringResource(R.string.force_stopped_text))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = onSeen) { Text(stringResource(R.string.ok)) }
            }
        }
    }
}

/** Light or dark as the phone is set; in the phone's own colours from Android 12, which has them. */
@Composable
fun ShuttleTheme(content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val colors = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
        val context = LocalContext.current
        if (dark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
    } else {
        if (dark) darkColorScheme() else lightColorScheme()
    }
    MaterialTheme(colorScheme = colors, content = content)
}

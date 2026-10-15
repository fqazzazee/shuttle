// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0 OR LicenseRef-PolyForm-Internal-Use-1.0.0

package io.github.fqazzazee.shuttle

import android.content.Intent
import android.os.Bundle
import android.os.Environment
import android.os.storage.StorageManager
import android.provider.Settings
import android.util.Log
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
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
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.core.content.edit
import androidx.core.net.toUri
import io.github.fqazzazee.shuttle.mobile.Draft
import io.github.fqazzazee.shuttle.mobile.Mobile
import io.github.fqazzazee.shuttle.mobile.Picker
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject

class MainActivity : ComponentActivity() {
    private var access by mutableStateOf(Access(files = false, localNetwork = false, notifications = false))
    private var firstRunSeen by mutableStateOf(false)
    private var forceStopped by mutableStateOf(false)
    private val pauses by lazy { Core.pauses(this) }
    private var pausedAll by mutableStateOf(false)
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
    private var about by mutableStateOf(false)
    private var help by mutableStateOf(false)
    // The wallpaper's colours (Material You) in place of shuttle's palette, as the About screen sets.
    private var wallpaperColors by mutableStateOf(false)
    // What the wizard marks; replaced with each new wizard, or another share.
    private var picker: Picker = Mobile.newPicker("/")
    private var marksRev by mutableStateOf(0)
    // The shuttle the wizard makes, until it is saved; why its last Next did not go on.
    private var draft: Draft? = null
    private var wizardProblem by mutableStateOf<String?>(null)
    // Bumped after the draft changes: its rows are read again.
    private var draftRev by mutableStateOf(0)
    // An edit that moves the shuttle, asked about before it is saved.
    private var askMoved by mutableStateOf(false)
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
        wallpaperColors = prefs.getBoolean(WALLPAPER_COLORS, false)
        pausedAll = pauses.all
        // Opening the app after a force stop starts syncing again (the screen holds the daemon,
        // which starts the service and plans the jobs); say that it had stopped.
        val lastSeen = prefs.getLong(LAST_EXIT_SEEN, 0)
        val exits = recentExits(this)
        forceStopped = forceStoppedSince(exits, lastSeen)
        if (forceStopped) Log.i("shuttle", "force stop noticed; syncing starts again")
        prefs.edit { putLong(LAST_EXIT_SEEN, newestExit(exits, lastSeen)) }
        setContent {
            ShuttleTheme(wallpaperColors) {
                Surface(Modifier.fillMaxSize()) {
                    if (!firstRunSeen) {
                        PermissionsScreen(access, ::allow) {
                            prefs.edit { putBoolean(FIRST_RUN_SEEN, true) }
                            firstRunSeen = true
                        }
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
                                    if (help) {
                                        BackHandler { help = false }
                                        HelpScreen { help = false }
                                    } else if (about) {
                                        BackHandler { about = false }
                                        val version = remember { packageManager.getPackageInfo(packageName, 0).versionName.orEmpty() }
                                        val notices = remember { readNotices(this@MainActivity) }
                                        // The APK is read whole for its checksum: off the main thread.
                                        val copy by produceState<ThisCopy?>(null) {
                                            value = withContext(Dispatchers.IO) { ThisCopy.of(this@MainActivity) }
                                        }
                                        AboutScreen(
                                            version, copy, notices, wallpaperColors.takeIf { hasWallpaperColors },
                                            { on -> wallpaperColors = on; prefs.edit { putBoolean(WALLPAPER_COLORS, on) } },
                                        ) { about = false }
                                    } else if (w != null) {
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
                                        val name = selected!!
                                        DetailScreen(
                                            Detail.parse(json),
                                            DetailActions(
                                                back = { selected = null }, edit = { editWizard(name) }, settings = { settingsOf = name },
                                                setEnabled = { setEnabled(name, it) }, delete = { deleteShuttle(name) },
                                            ),
                                        )
                                    } else {
                                        Main(
                                            access, Core.statuses, Core.startError, forceStopped, { forceStopped = false },
                                            ::allow, pausedAll, actions, ::openWizard, { help = true },
                                        ) { about = true }
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
        enable = { setEnabled(it, true) },
    )

    // The config file is written off the main thread; the daemon follows it, and the list with it.
    private fun setEnabled(name: String, on: Boolean) {
        Core.async { Core.current?.setEnabled(name, on) }
    }

    /** Deletes a shuttle from the config, and leaves its screen: null, or why it was not. */
    private fun deleteShuttle(name: String): String? = try {
        Core.current?.deleteShuttle(name) ?: throw IllegalStateException(getString(R.string.add_no_daemon))
        selected = null
        null
    } catch (e: Exception) {
        e.message
    }

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
        CompositionLocalProvider(LocalWizardEditing provides if (w.editing) w.name else "") { WizardScreen(w) }
    }

    @Composable
    private fun WizardScreen(w: WizardState) {
        when (w.step) {
            WizardStep.SHARE -> {
                val shares = runCatching { Core.current?.sharesJSON() }.getOrNull()?.let(ShareRow::parseAll) ?: emptyList()
                WizardShareStep(
                    shares, w.share, w.name, wizardProblem, { wizard = w.copy(name = it) }, ::chooseShare,
                    { wizardNext(w.copy(step = WizardStep.FOLDER)) { it.setName(w.name.trim()) } }, ::closeWizard,
                )
            }
            WizardStep.FOLDER -> {
                // Read off the main thread: the share is reached over the network.
                val listing = listing(w.share, w.dir)
                WizardFolderStep(w, listing, { wizard = it }, { name -> makeShareFolder(w.share, w.dir, name) }, { browseTry++ }, ::wizardBack) {
                    // Marks outside the folder now chosen are kept, and do not count.
                    picker.setRoot(w.dir)
                    wizard = w.copy(step = WizardStep.WHAT, inner = if (within(w.inner, w.dir)) w.inner else w.dir)
                }
            }
            WizardStep.WHAT -> {
                val listing = listing(w.share, w.inner)
                // Read again when a mark changes: the picker holds them.
                val view = marksRev.let { MarksView(picker.mode().toInt(), { picker.synced(it).toInt() }, marks()) }
                WizardWhatStep(
                    w, listing, view,
                    { picker.setMode(it.toLong()); marksRev++ },
                    { picker.toggle(it); marksRev++ },
                    { wizard = it }, { browseTry++ }, ::wizardBack,
                ) { wizard = w.copy(step = WizardStep.LOCAL, local = w.local.ifEmpty { volumes.first().path }) }
            }
            WizardStep.LOCAL -> {
                // Local, but on storage that can be slow to wake: off the main thread too.
                val listing = readListing(w.local) { Mobile.localJSON(w.local) }
                val storage = remember { volumes }
                WizardLocalStep(
                    w, storage, listing, wizardProblem,
                    { wizardProblem = null; wizard = w.copy(local = it) },
                    { name -> makeFolder(w.local, name) },
                    { browseTry++ }, ::wizardBack,
                ) { wizardNext(w.copy(step = WizardStep.PROFILE)) { it.setFolders(w.share, w.dir, w.local, picker.selectionJSON()) } }
            }
            WizardStep.PROFILE -> {
                val profiles = runCatching { Core.current?.profilesJSON() }.getOrNull()?.let(ProfileRow::parseAll) ?: emptyList()
                WizardProfileStep(profiles, w.profile, { wizard = w.copy(profile = it) }, ::wizardBack) {
                    wizardNext(w.copy(step = WizardStep.SETTINGS)) { it.setProfile(w.profile) }
                    draftRev++
                }
            }
            WizardStep.SETTINGS -> {
                val rows = draftRev.let { runCatching { draft?.settingsJSON() }.getOrNull() } ?: "[]"
                SettingsScreen(
                    stringResource(R.string.wizard_settings_title, w.name.trim()), Setting.parseAll(rows),
                    { key, text ->
                        try {
                            draft?.set(key, text)
                            draftRev++
                            // Why the last Save failed may be what was just set.
                            wizardProblem = null
                            null
                        } catch (e: Exception) {
                            e.message
                        }
                    },
                    head = { WizardSaveHead(w.profile, wizardProblem) { saveDraft(false) } },
                    onBack = ::wizardBack,
                )
                if (askMoved) MovedDialog(w.name, { askMoved = false; saveDraft(true) }) { askMoved = false }
            }
        }
    }

    private fun marks(): List<String> = JSONArray(picker.marksJSON()).let { a -> (0 until a.length()).map { a.getString(it) } }

    /** Lists dir on the share off the main thread, as the picker is told; null while it is read. */
    @Composable
    private fun listing(share: String, dir: String): Listing? = readListing(share, dir) {
        Core.current?.browseJSON(share, dir)?.also { picker.listed(dir, it) }
    }

    /** What read gives off the main thread, again for new keys or Try again; null while it is read. */
    @Composable
    private fun readListing(vararg keys: Any, read: () -> String?): Listing? {
        val listing by produceState<Listing?>(null, *keys, browseTry) {
            value = null
            value = withContext(Dispatchers.IO) {
                try {
                    val json = read()
                    if (json == null) Listing(null, getString(R.string.add_no_daemon)) else Listing(FolderEntry.parseAll(json), null)
                } catch (e: Exception) {
                    Listing(null, e.message)
                }
            }
        }
        return listing
    }

    /** The phone's storage the wizard browses: its own first, then any SD card. */
    private val volumes: List<Volume>
        get() {
            val found = getSystemService(StorageManager::class.java).storageVolumes
                .filter { it.state == Environment.MEDIA_MOUNTED }
                .mapNotNull { v -> v.directory?.let { Volume(v.getDescription(this), it.path) } }
            return found.ifEmpty { listOf(Volume(getString(R.string.wizard_phone_storage), Environment.getExternalStorageDirectory().path)) }
        }

    /** Makes the folder name in dir on the share, off the main thread, and opens it: null, or why it was not made. */
    private suspend fun makeShareFolder(share: String, dir: String, name: String): String? = try {
        val made = withContext(Dispatchers.IO) { Core.current?.makeShareFolder(share, dir, name) }
            ?: throw IllegalStateException(getString(R.string.add_no_daemon))
        wizard = wizard?.copy(dir = made)
        null
    } catch (e: Exception) {
        e.message
    }

    /** Makes the folder name in dir and opens it: null, or why it was not made. */
    private fun makeFolder(dir: String, name: String): String? = try {
        val made = Mobile.makeFolder(dir, name)
        wizard = wizard?.copy(local = made)
        null
    } catch (e: Exception) {
        e.message
    }

    // Marks on another share mean nothing there: they start again, as in the TUI.
    private fun chooseShare(share: String) {
        val w = wizard ?: return
        if (share == w.share) return
        wizard = w.copy(share = share, dir = "/", inner = "/")
        picker = Mobile.newPicker("/")
        marksRev++
    }

    private fun openWizard() {
        picker = Mobile.newPicker("/")
        marksRev++
        draft = Core.current?.newDraft()
        wizardProblem = null
        wizard = WizardState()
    }

    /** Goes on to next once check, on the draft, passes; else says why, and stays. */
    private fun wizardNext(next: WizardState, check: (Draft) -> Unit) {
        val d = draft ?: Core.current?.newDraft()?.also { draft = it }
        wizardProblem = if (d == null) {
            getString(R.string.add_no_daemon)
        } else {
            try {
                check(d)
                null
            } catch (e: Exception) {
                e.message
            }
        }
        if (wizardProblem == null) wizard = next
    }

    /**
     * Saves the wizard's shuttle and closes it; the daemon follows the file. Else says why. An edit
     * that moves the shuttle is asked about first, unless that was answered.
     */
    private fun saveDraft(moveAsked: Boolean) {
        wizardProblem = try {
            val d = draft ?: throw IllegalStateException(getString(R.string.add_no_daemon))
            if (!moveAsked && d.moved()) {
                askMoved = true
                return
            }
            d.save()
            null
        } catch (e: Exception) {
            e.message
        }
        if (wizardProblem == null) closeWizard()
    }

    /** Opens the wizard on a saved shuttle, at its first step, with its folders, marks and profile. */
    private fun editWizard(name: String) {
        val d = try {
            Core.current?.editDraft(name)
        } catch (e: Exception) {
            Log.w("shuttle", "edit $name: ${e.message}")
            null
        } ?: return
        val o = JSONObject(d.json())
        val folder = o.getString("Folder")
        picker = Mobile.newPicker(folder).also { it.setSelection(o.getJSONObject("Selection").toString()) }
        marksRev++
        draft = d
        wizardProblem = null
        wizard = WizardState(
            share = o.getString("Share"), dir = folder, inner = folder, local = o.getString("Local"),
            name = name, profile = o.optString("Profile"), editing = true,
        )
    }

    private fun wizardBack() {
        val w = wizard ?: return
        wizardProblem = null
        when (w.step) {
            WizardStep.SHARE -> closeWizard()
            WizardStep.FOLDER -> wizard = w.copy(step = WizardStep.SHARE)
            WizardStep.WHAT -> wizard = w.copy(step = WizardStep.FOLDER)
            WizardStep.LOCAL -> wizard = w.copy(step = WizardStep.WHAT)
            WizardStep.PROFILE -> wizard = w.copy(step = WizardStep.LOCAL)
            WizardStep.SETTINGS -> wizard = w.copy(step = WizardStep.PROFILE)
        }
    }

    // The share stays open between listings; let it go off the main thread, as a listing in
    // progress holds it.
    private fun closeWizard() {
        wizard = null
        draft = null
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
        const val WALLPAPER_COLORS = "wallpaper_colors"
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
    onHelp: () -> Unit,
    onAbout: () -> Unit,
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
        Row {
            TextButton(onClick = onHelp) { Text(stringResource(R.string.help)) }
            TextButton(onClick = onAbout) { Text(stringResource(R.string.about)) }
        }
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


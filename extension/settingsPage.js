// SPDX-License-Identifier: GPL-2.0-or-later

// The preferences window's pages: the shuttles, and each one's settings,
// edited through the daemon (Daemon.setShuttleSetting), which writes them
// to config.toml as the TUI does. Adding shuttles, shares and passwords is
// the TUI's. It imports nothing from GNOME Shell, so a test can build it in
// plain gjs.

import Adw from 'gi://Adw';
import GLib from 'gi://GLib';
import Gtk from 'gi://Gtk';

import {choiceItems, inheritedText, reason, sections, selectedIndex, subtitle, valueAt} from './lib/settings.js';
import {modeArrow} from './lib/shuttles.js';

// fillWindow fills an Adw.PreferencesWindow with the shuttles, each opening
// its settings, following daemon as it comes and goes.
export function fillWindow(window, daemon) {
    window.set_search_enabled(false);
    const page = new Adw.PreferencesPage({title: 'Shuttles', icon_name: 'emblem-synchronizing-symbolic'});
    window.add(page);
    let group = null;
    const show = () => {
        if (group)
            page.remove(group);
        group = shuttlesGroup(window, daemon);
        page.add(group);
    };
    const ids = ['changed', 'listed', 'shuttle-removed'].map(signal => daemon.connect(signal, show));
    // A new shuttle; a changed one only when its mode is not as shown.
    ids.push(daemon.connect('shuttle-changed', (_daemon, name) => {
        if (group?.shown.get(name) !== daemon.shuttles.get(name)?.Mode)
            show();
    }));
    window.connect('close-request', () => {
        ids.forEach(id => daemon.disconnect(id));
        return false;
    });
    show();
}

// shuttlesGroup lists the shuttles, or says why there are none to show.
function shuttlesGroup(window, daemon) {
    const group = new Adw.PreferencesGroup({
        title: 'Shuttles',
        description: 'Each shuttle\'s settings. Adding shuttles, shares and their passwords is in the ' +
            'terminal interface: run shuttle.',
    });
    group.shown = new Map(); // name -> mode, as listed
    if (!daemon.running) {
        group.add(new Adw.ActionRow({
            title: 'The shuttle service is not running',
            subtitle: 'Start it with: systemctl --user start shuttle',
        }));
        return group;
    }
    if (!daemon.listed)
        return group;
    const names = [...daemon.shuttles.keys()].sort();
    if (names.length === 0) {
        group.add(new Adw.ActionRow({title: 'No shuttles yet', subtitle: 'Add one in the terminal interface: run shuttle'}));
        return group;
    }
    for (const name of names) {
        const mode = daemon.shuttles.get(name).Mode;
        group.shown.set(name, mode);
        const row = new Adw.ActionRow({title: name, subtitle: `${modeArrow(mode)} ${mode}`, activatable: true});
        row.add_suffix(new Gtk.Image({icon_name: 'go-next-symbolic'}));
        row.connect('activated', () => window.push_subpage(shuttlePage(window, daemon, name)));
        group.add(row);
    }
    return group;
}

// shuttlePage is one shuttle's settings, read again after each change and
// each reload of the config, so it shows what is on disk.
export function shuttlePage(window, daemon, name) {
    const prefs = new Adw.PreferencesPage();
    const view = new Adw.ToolbarView({content: prefs});
    view.add_top_bar(new Adw.HeaderBar());
    const nav = new Adw.NavigationPage({title: name, child: view, tag: name});
    const widgets = new Map(); // key -> {update(row)}
    let updating = false; // the page setting its widgets, not the user

    const set = async (key, value) => {
        try {
            await daemon.setShuttleSetting(name, key, value);
        } catch (e) {
            window.add_toast(new Adw.Toast({title: GLib.markup_escape_text(reason(e.message), -1), timeout: 6}));
        }
        await load();
    };
    const load = async () => {
        let rows;
        try {
            rows = await daemon.shuttleSettings(name);
        } catch (e) {
            window.add_toast(new Adw.Toast({title: GLib.markup_escape_text(reason(e.message), -1), timeout: 6}));
            return;
        }
        if (widgets.size === 0) {
            for (const s of sections(rows)) {
                const group = new Adw.PreferencesGroup({title: s.title});
                for (const row of s.rows) {
                    const w = row.kind === 'choice' ? choiceRow(row, set, () => updating) : entryRow(row, set);
                    widgets.set(row.key, w);
                    group.add(w.widget);
                }
                prefs.add(group);
            }
        }
        updating = true;
        for (const row of rows)
            widgets.get(row.key)?.update(row);
        updating = false;
    };
    nav.load = load; // for a test, to wait on
    let id = 0;
    nav.connect('shown', () => {
        id = daemon.connect('config-reloaded', () => load());
    });
    nav.connect('hidden', () => {
        if (id)
            daemon.disconnect(id);
        id = 0;
    });
    load();
    return nav;
}

// choiceRow is a drop-down of a choice row: not set, then each choice. A
// drop-down beside the row rather than an Adw.ComboRow: it is as wide as
// its longest choice, where a combo row cuts the value short to make room
// for the hint.
function choiceRow(row, set, updating) {
    const widget = new Adw.ActionRow({title: row.label});
    widget.key = row.key;
    const dropdown = Gtk.DropDown.new_from_strings(choiceItems(row));
    dropdown.valign = Gtk.Align.CENTER;
    widget.add_suffix(dropdown);
    widget.dropdown = dropdown; // for a test
    let current = row;
    dropdown.connect('notify::selected', () => {
        if (!updating())
            set(current.key, valueAt(current, dropdown.selected));
    });
    return {
        widget,
        update(r) {
            current = r;
            dropdown.model = Gtk.StringList.new(choiceItems(r));
            dropdown.selected = selectedIndex(r);
            widget.subtitle = subtitle(r);
            widget.sensitive = !r.off;
        },
    };
}

// entryRow is a typed row: text, a number or a list, set on Enter or on
// leaving it; what applies when not set shows in the empty entry.
function entryRow(row, set) {
    const widget = new Adw.ActionRow({title: row.label});
    widget.key = row.key;
    const entry = new Gtk.Entry({valign: Gtk.Align.CENTER, width_chars: 16});
    const reset = new Gtk.Button({
        icon_name: 'edit-clear-symbolic', valign: Gtk.Align.CENTER, tooltip_text: 'Back to what applies when not set',
        css_classes: ['flat'],
    });
    widget.add_suffix(entry);
    widget.add_suffix(reset);
    let current = row;
    const apply = () => {
        const text = entry.text.trim();
        if (text !== current.value)
            set(current.key, text);
    };
    entry.connect('activate', apply);
    const focus = new Gtk.EventControllerFocus();
    focus.connect('leave', apply);
    entry.add_controller(focus);
    reset.connect('clicked', () => set(current.key, ''));
    widget.entry = entry; // for a test
    return {
        widget,
        update(r) {
            current = r;
            entry.text = r.value;
            entry.placeholder_text = inheritedText(r);
            widget.subtitle = subtitle(r);
            widget.sensitive = !r.off;
            reset.visible = r.value !== '';
        },
    };
}

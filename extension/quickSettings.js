// SPDX-License-Identifier: GPL-2.0-or-later

import GObject from 'gi://GObject';
import Shell from 'gi://Shell';
import St from 'gi://St';

import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import * as QuickSettings from 'resource:///org/gnome/shell/ui/quickSettings.js';

import {modeArrow} from './lib/shuttles.js';
import {NOT_RUNNING, presentation} from './lib/state.js';

// APP_ID is the desktop file `make install` puts in place: it runs
// `shuttle` in a terminal (packaging/desktop).
const APP_ID = 'io.github.fqazzazee.Shuttle.desktop';

// complain tells the user a request to the daemon failed.
function complain(what, e) {
    Main.notifyError(`Shuttle: cannot ${what}`, e.message);
}

// ShuttleSwitch is one shuttle in the toggle's menu: a switch that pauses
// (off) and resumes (on) it, and a button that syncs it now.
class ShuttleSwitch extends PopupMenu.PopupSwitchMenuItem {
    static {
        GObject.registerClass(this);
    }

    constructor(name, daemon) {
        super(name, false);
        this._name = name;
        this._button = new St.Button({
            style_class: 'icon-button shuttle-sync-now',
            child: new St.Icon({icon_name: 'view-refresh-symbolic', style_class: 'popup-menu-icon'}),
            can_focus: true,
            accessible_name: `Sync ${name} now`,
        });
        this.insert_child_below(this._button, this._statusBin);
        // The name takes the room, so the button sits by the switch.
        this.label.x_expand = true;
        this._statusBin.x_expand = false;
        this._button.connect('clicked', () => {
            daemon.syncNow(name).catch(e => complain(`sync ${name}`, e));
        });
        // The switch says it was toggled when update() sets it too: only
        // the user's toggles go to the daemon. A request it takes, even one
        // that changes nothing, comes back as a status change and resets the
        // switch; a refused one does not, so the switch is reset here.
        this._updating = false;
        this.connect('toggled', (_item, on) => {
            if (this._updating)
                return;
            daemon.setPaused(name, !on).catch(e => {
                complain(on ? `resume ${name}` : `pause ${name}`, e);
                const st = daemon.shuttles.get(name);
                if (st && !this._destroyed)
                    this.update(st, daemon.paused);
            });
        });
        this.connect('destroy', () => (this._destroyed = true));
    }

    // update shows st, the daemon's status for this shuttle, and whether
    // every shuttle is paused.
    update(st, allPaused) {
        this.status = st;
        this.label.text = `${this._name}  ${modeArrow(st.Mode)}`;
        const off = st.State === 'disabled';
        // A shuttle turned off in the config has nothing to pause or sync:
        // the TUI or the command line turns it back on.
        this.setStatus(off ? presentation('disabled').label : null);
        this._button.visible = !off;
        this._updating = true;
        this.setToggleState(!st.Paused);
        this._updating = false;
        // With every shuttle paused, a shuttle cannot be resumed by itself:
        // the toggle resumes them.
        this.setSensitive(!allPaused && !off);
        this._button.reactive = !allPaused;
    }

    // shown is the item as on screen, for the headless tests to read.
    get shown() {
        return {name: this._name, on: this.state, off: !this._button.visible, label: this.label.text,
            sensitive: this.sensitive};
    }
}

// Toggle is the Quick Settings toggle: its button pauses and resumes every
// shuttle, its menu each one, and opens shuttle in a terminal.
class Toggle extends QuickSettings.QuickMenuToggle {
    static {
        GObject.registerClass(this);
    }

    constructor(icon, daemon) {
        super({title: 'Shuttle', gicon: icon, toggleMode: false});
        this._daemon = daemon;
        this.menu.setHeader(icon, 'Shuttle');
        this._section = new PopupMenu.PopupMenuSection();
        this.menu.addMenuItem(this._section);
        this._items = new Map();
        this.menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
        this.menu.addAction('Open shuttle', () => this._open());

        // checked follows the daemon, not the click: the toggle never shows
        // a state the daemon did not take.
        this.connect('clicked', () => {
            if (!daemon.running)
                return;
            const pause = !daemon.paused;
            daemon.setPaused('', pause).catch(e => complain(pause ? 'pause' : 'resume', e));
        });
        daemon.connectObject(
            'changed', () => this._update(),
            'shuttle-changed', (_daemon, name) => {
                this._set(name);
                this._update();
            },
            'shuttle-removed', (_daemon, name) => {
                this._remove(name);
                this._update();
            },
            this);
        for (const name of daemon.shuttles.keys())
            this._set(name);
        this._update();
    }

    _update() {
        const d = this._daemon;
        const state = d.running ? d.state || NOT_RUNNING : NOT_RUNNING;
        this.checked = d.running && !d.paused;
        this.subtitle = presentation(state).label;
        // One paused shuttle makes the daemon's overall state "paused", but
        // the toggle is still on: say how many.
        const paused = [...d.shuttles.values()].filter(st => st.Paused).length;
        if (state === 'paused' && !d.paused && paused > 0)
            this.subtitle = `${paused} of ${d.shuttles.size} paused`;
        this.menu.setHeader(this.gicon, 'Shuttle', this.subtitle);
        // "Every shuttle paused" is the daemon's Paused, not in their status.
        this._items.forEach(item => item.update(item.status, d.paused));
    }

    _set(name) {
        let item = this._items.get(name);
        if (!item) {
            item = new ShuttleSwitch(name, this._daemon);
            this._items.set(name, item);
            const names = [...this._items.keys()].sort();
            this._section.addMenuItem(item, names.indexOf(name));
        }
        item.update(this._daemon.shuttles.get(name), this._daemon.paused);
    }

    _remove(name) {
        this._items.get(name)?.destroy();
        this._items.delete(name);
    }

    _open() {
        const app = Shell.AppSystem.get_default().lookup_app(APP_ID);
        if (!app) {
            Main.notifyError('Shuttle: cannot open shuttle',
                `${APP_ID} is not installed: \`make install\` puts it in place.`);
            return;
        }
        Main.panel.closeQuickSettings();
        app.activate();
    }

    // shownItems is the menu's shuttles as on screen, for the headless
    // tests to read.
    get shownItems() {
        return [...this._items.keys()].sort().map(name => this._items.get(name).shown);
    }

    destroy() {
        this._daemon.disconnectObject(this);
        super.destroy();
    }
}

// QuickSettingsIndicator puts the toggle in Quick Settings. It shows no
// icon of its own: the top-bar indicator is that.
export class QuickSettingsIndicator extends QuickSettings.SystemIndicator {
    static {
        GObject.registerClass(this);
    }

    constructor(icon, daemon) {
        super();
        this.toggle = new Toggle(icon, daemon);
        this.quickSettingsItems.push(this.toggle);
    }

    destroy() {
        this.quickSettingsItems.forEach(item => item.destroy());
        super.destroy();
    }
}

// SPDX-License-Identifier: GPL-2.0-or-later

// The controls the top bar's dropdown and the Quick Settings toggle's menu
// both have: a switch that pauses and resumes every shuttle, a switch and a
// Sync now button per shuttle, and Open shuttle.

import Clutter from 'gi://Clutter';
import GObject from 'gi://GObject';
import Shell from 'gi://Shell';
import St from 'gi://St';

import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

import {modeArrow} from './lib/shuttles.js';
import {presentation} from './lib/state.js';

// APP_ID is the desktop file `make install` puts in place: it runs
// `shuttle` in a terminal (packaging/desktop).
const APP_ID = 'io.github.fqazzazee.Shuttle.desktop';

// complain tells the user a request to the daemon failed.
export function complain(what, e) {
    Main.notifyError(`Shuttle: cannot ${what}`, e.message);
}

// openShuttle opens shuttle in a terminal.
export function openShuttle() {
    const app = Shell.AppSystem.get_default().lookup_app(APP_ID);
    if (!app) {
        Main.notifyError('Shuttle: cannot open shuttle',
            `${APP_ID} is not installed: \`make install\` puts it in place.`);
        return;
    }
    Main.panel.closeQuickSettings();
    app.activate();
}

// AllSwitch pauses (off) and resumes (on) every shuttle, as the Quick
// Settings toggle's button does.
export class AllSwitch extends PopupMenu.PopupSwitchMenuItem {
    static {
        GObject.registerClass(this);
    }

    constructor(daemon) {
        super('All shuttles', false);
        this._daemon = daemon;
        // As with ShuttleSwitch: only the user's toggles go to the daemon,
        // and a refused one puts the switch back.
        this._updating = false;
        this.connect('toggled', (_item, on) => {
            if (this._updating)
                return;
            daemon.setPaused('', !on).catch(e => {
                complain(on ? 'resume' : 'pause', e);
                if (!this._destroyed)
                    this.update();
            });
        });
        this.connect('destroy', () => (this._destroyed = true));
    }

    // update shows the daemon's state: hidden while it is not running.
    update() {
        const d = this._daemon;
        this.visible = d.running;
        this._updating = true;
        this.setToggleState(d.running && !d.paused);
        this._updating = false;
    }
}

// ShuttleSwitch is one shuttle: a switch that pauses (off) and resumes (on)
// it, and a button that syncs it now.
export class ShuttleSwitch extends PopupMenu.PopupSwitchMenuItem {
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
            // A round button, however many lines the dropdown's row has.
            y_align: Clutter.ActorAlign.CENTER,
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
        // the toggle, or All shuttles, resumes them.
        this.setSensitive(!allPaused && !off);
        this._button.reactive = !allPaused;
    }

    // shown is the item as on screen, for the headless tests to read.
    get shown() {
        return {name: this._name, on: this.state, off: !this._button.visible, label: this.label.text,
            sensitive: this.sensitive};
    }
}

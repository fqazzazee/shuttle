// SPDX-License-Identifier: GPL-2.0-or-later

import GObject from 'gi://GObject';

import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import * as QuickSettings from 'resource:///org/gnome/shell/ui/quickSettings.js';

import {ShuttleSwitch, complain, openShuttle} from './controls.js';
import {NOT_RUNNING, presentation} from './lib/state.js';

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
        this.menu.addAction('Open shuttle', () => openShuttle());

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

// SPDX-License-Identifier: GPL-2.0-or-later

// shuttle's preferences, in GNOME's Extensions app: each shuttle's settings,
// edited through the daemon. See settingsPage.js.

import {ExtensionPreferences} from 'resource:///org/gnome/Shell/Extensions/js/extensions/prefs.js';

import {Daemon} from './daemon.js';
import {fillWindow} from './settingsPage.js';

export default class ShuttlePreferences extends ExtensionPreferences {
    fillPreferencesWindow(window) {
        const daemon = new Daemon();
        fillWindow(window, daemon);
        window.connect('close-request', () => {
            daemon.destroy();
            return false;
        });
    }
}

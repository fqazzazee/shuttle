// SPDX-License-Identifier: GPL-2.0-or-later

import Clutter from 'gi://Clutter';
import Gio from 'gi://Gio';
import GObject from 'gi://GObject';
import St from 'gi://St';

import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';

import {rates} from './lib/format.js';
import {NOT_RUNNING, SHUTTLE_ICON, presentation} from './lib/state.js';

// The daemon's D-Bus API: internal/bus/server.go.
const BUS_NAME = 'io.github.fqazzazee.Shuttle';
const OBJECT_PATH = '/io/github/fqazzazee/Shuttle';
const INTERFACE = 'io.github.fqazzazee.Shuttle1';

// Indicator is the top-bar icon, and the transfer rates beside it while
// something moves. It follows the daemon over D-Bus and changes nothing: a
// client of the daemon, like the TUI.
class Indicator extends PanelMenu.Button {
    static {
        GObject.registerClass(this);
    }

    constructor(ext) {
        super(0.5, 'Shuttle');
        this._shuttleIcon = new Gio.FileIcon({file: ext.dir.get_child('icons').get_child('shuttle-symbolic.svg')});
        this._icon = new St.Icon({style_class: 'system-status-icon'});
        this._rates = new St.Label({y_align: Clutter.ActorAlign.CENTER, visible: false});
        const box = new St.BoxLayout();
        box.add_child(this._icon);
        box.add_child(this._rates);
        this.add_child(box);
        // What is on show, for the headless tests to read.
        this.shownState = '';
        this.shownRates = '';
        this.pulsing = false;
        this._show(NOT_RUNNING);

        // No auto-start: the daemon is a systemd user service, and the
        // indicator waits for it rather than starting it.
        this._cancellable = new Gio.Cancellable();
        Gio.DBusProxy.new_for_bus(Gio.BusType.SESSION, Gio.DBusProxyFlags.DO_NOT_AUTO_START, null,
            BUS_NAME, OBJECT_PATH, INTERFACE, this._cancellable, (_source, result) => {
                try {
                    this._proxy = Gio.DBusProxy.new_for_bus_finish(result);
                } catch (e) {
                    if (!e.matches(Gio.IOErrorEnum, Gio.IOErrorEnum.CANCELLED))
                        console.error(`shuttle: cannot follow the daemon: ${e.message}`);
                    return;
                }
                // The proxy loads the properties before it says the owner
                // changed, so both come down to reading them again.
                this._proxy.connectObject(
                    'g-properties-changed', () => this._update(),
                    'notify::g-name-owner', () => this._update(),
                    this);
                this._update();
            });
    }

    _update() {
        const owner = this._proxy.g_name_owner;
        const get = name => owner ? this._proxy.get_cached_property(name)?.unpack() : null;
        this._show(get('State') || NOT_RUNNING);
        this._showRates(rates(get('RateUp') ?? 0, get('RateDown') ?? 0));
    }

    _show(state) {
        if (state === this.shownState)
            return;
        const {icon, label} = presentation(state);
        this._icon.gicon = icon === SHUTTLE_ICON ? this._shuttleIcon : new Gio.ThemedIcon({name: icon});
        this.accessible_name = `Shuttle: ${label}`;
        this.shownState = state;
        this._pulse(state === 'running');
    }

    _showRates(text) {
        this._rates.text = text;
        this._rates.visible = text !== '';
        this.shownRates = text;
    }

    // _pulse fades the icon in and out while shuttles sync. With animations
    // turned off in the accessibility settings, ease() does nothing: the
    // rates and the accessible name still say it.
    _pulse(on) {
        if (on === this.pulsing)
            return;
        this.pulsing = on;
        this._icon.remove_transition('opacity');
        this._icon.opacity = 255;
        if (on) {
            this._icon.ease({
                opacity: 96, duration: 1000, mode: Clutter.AnimationMode.EASE_IN_OUT_QUAD,
                repeatCount: -1, autoReverse: true,
            });
        }
    }

    destroy() {
        this._cancellable.cancel();
        this._proxy?.disconnectObject(this);
        this._proxy = null;
        super.destroy();
    }
}

export default class ShuttleExtension extends Extension {
    enable() {
        this._indicator = new Indicator(this);
        Main.panel.addToStatusArea(this.uuid, this._indicator);
    }

    disable() {
        this._indicator?.destroy();
        this._indicator = null;
    }
}

// SPDX-License-Identifier: GPL-2.0-or-later

import Clutter from 'gi://Clutter';
import Gio from 'gi://Gio';
import GObject from 'gi://GObject';
import St from 'gi://St';

import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

import {rates} from './lib/format.js';
import {row} from './lib/shuttles.js';
import {NOT_RUNNING, SHUTTLE_ICON, presentation} from './lib/state.js';

// The daemon's D-Bus API: internal/bus/server.go.
const BUS_NAME = 'io.github.fqazzazee.Shuttle';
const OBJECT_PATH = '/io/github/fqazzazee/Shuttle';
const INTERFACE = 'io.github.fqazzazee.Shuttle1';

// ShuttleItem is one shuttle in the dropdown: its state, name and mode,
// when it last ran, and what it is doing or what needs the user. It shows
// and does nothing more: the controls are in Quick Settings.
class ShuttleItem extends PopupMenu.PopupBaseMenuItem {
    static {
        GObject.registerClass(this);
    }

    constructor(name, iconFor) {
        super({reactive: false, can_focus: false});
        // Not a control, but not something switched off either: the theme
        // dims inactive items as if they were.
        this.remove_style_class_name('popup-inactive-menu-item');
        this._name = name;
        this._iconFor = iconFor;
        this._icon = new St.Icon({style_class: 'popup-menu-icon'});
        this._title = new St.Label({x_expand: true});
        this._when = new St.Label({style_class: 'shuttle-row-when'});
        this._detail = new St.Label({style_class: 'shuttle-row-detail', visible: false});
        this._detail.clutter_text.line_wrap = true;
        this._when.opacity = this._detail.opacity = 190; // below the name, in light themes and dark
        const top = new St.BoxLayout();
        top.add_child(this._title);
        top.add_child(this._when);
        const text = new St.BoxLayout({orientation: Clutter.Orientation.VERTICAL, x_expand: true});
        text.add_child(top);
        text.add_child(this._detail);
        this.add_child(this._icon);
        this.add_child(text);
    }

    // update shows st, the daemon's status, at now (ms).
    update(st, now) {
        this.status = st;
        this.row = row(this._name, st, now);
        const {icon, label} = presentation(this.row.state);
        this._icon.gicon = this._iconFor(icon);
        this._title.text = this.row.title;
        this._when.text = this.row.when;
        this._detail.text = this.row.detail;
        this._detail.visible = this.row.detail !== '';
        this.accessible_name = [this._name, label, this.row.when, this.row.detail].filter(Boolean).join(', ');
    }

    // shown is the item as on screen, read back from its labels.
    get shown() {
        return {
            name: this._name, state: this.row.state, title: this._title.text, when: this._when.text,
            detail: this._detail.visible ? this._detail.text : '',
        };
    }
}

// Indicator is the top-bar icon, the transfer rates beside it while
// something moves, and a dropdown of the shuttles. It follows the daemon
// over D-Bus and changes nothing: a client of the daemon, like the TUI.
class Indicator extends PanelMenu.Button {
    static {
        GObject.registerClass(this);
    }

    constructor(ext) {
        super(0.5, 'Shuttle');
        const shuttleIcon = new Gio.FileIcon({file: ext.dir.get_child('icons').get_child('shuttle-symbolic.svg')});
        this._iconFor = name => name === SHUTTLE_ICON ? shuttleIcon : new Gio.ThemedIcon({name});
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

        // The dropdown: a line for when there is no shuttle to show, then
        // one item per shuttle, by name.
        this._items = new Map();
        this._message = new PopupMenu.PopupMenuItem('', {reactive: false, can_focus: false});
        this.menu.addMenuItem(this._message);
        this._listed = false;
        this._showMessage();
        // "5m ago" moves on while the menu is closed.
        this.menu.connectObject('open-state-changed', (_menu, open) => {
            if (open)
                this._items.forEach(item => item.update(item.status, Date.now()));
        }, this);

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
                    'notify::g-name-owner', () => this._ownerChanged(),
                    'g-signal', (_proxy, _sender, signal, params) => this._signal(signal, params),
                    this);
                this._ownerChanged();
            });
    }

    _ownerChanged() {
        this._update();
        this._items.forEach((_item, name) => this._remove(name));
        this._listed = false;
        if (this._proxy.g_name_owner)
            this._list();
        this._showMessage();
    }

    // _list fetches every shuttle's status. A change signalled while the
    // answer is on its way may be overwritten by the older answer; the
    // shuttle's next change puts it right.
    _list() {
        this._proxy.call('ListShuttles', null, Gio.DBusCallFlags.NONE, -1, this._cancellable, (proxy, result) => {
            let all;
            try {
                [all] = proxy.call_finish(result).recursiveUnpack();
            } catch (e) {
                if (!e.matches(Gio.IOErrorEnum, Gio.IOErrorEnum.CANCELLED))
                    console.warn(`shuttle: cannot list the shuttles: ${e.message}`);
                return;
            }
            if (!this._proxy?.g_name_owner)
                return;
            for (const [name, st] of Object.entries(all))
                this._set(name, st);
            for (const name of [...this._items.keys()]) {
                if (!Object.hasOwn(all, name))
                    this._remove(name);
            }
            this._listed = true;
            this._showMessage();
        });
    }

    _signal(signal, params) {
        if (signal === 'ShuttleChanged') {
            const [name, st] = params.recursiveUnpack();
            this._set(name, st);
        } else if (signal === 'ShuttleRemoved') {
            const [name] = params.recursiveUnpack();
            this._remove(name);
        } else {
            return;
        }
        this._showMessage();
    }

    _set(name, st) {
        let item = this._items.get(name);
        if (!item) {
            item = new ShuttleItem(name, this._iconFor);
            this._items.set(name, item);
            const names = [...this._items.keys()].sort();
            this.menu.addMenuItem(item, 1 + names.indexOf(name)); // after the message line
        }
        item.update(st, Date.now());
    }

    _remove(name) {
        this._items.get(name)?.destroy();
        this._items.delete(name);
    }

    _showMessage() {
        let text = '';
        if (!this._proxy?.g_name_owner)
            text = presentation(NOT_RUNNING).label;
        else if (this._listed && this._items.size === 0)
            text = 'No shuttles yet: run shuttle in a terminal to add one';
        this._message.label.text = text;
        this._message.visible = text !== '';
    }

    // shownRows is the dropdown's shuttles as on screen, for the headless
    // tests to read.
    get shownRows() {
        return [...this._items.keys()].sort().map(name => this._items.get(name).shown);
    }

    get shownMessage() {
        return this._message.visible ? this._message.label.text : '';
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
        this._icon.gicon = this._iconFor(icon);
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
        this.menu.disconnectObject(this);
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

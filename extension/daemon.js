// SPDX-License-Identifier: GPL-2.0-or-later

// Daemon follows the shuttle daemon over D-Bus for the indicator and the
// Quick Settings toggle, so both show the same thing from one connection,
// and edits shuttles' settings for the preferences window. It imports
// nothing from GNOME Shell: the preferences run in a process of their own.

import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import GObject from 'gi://GObject';

// The daemon's D-Bus API: internal/bus/server.go.
const BUS_NAME = 'io.github.fqazzazee.Shuttle';
const OBJECT_PATH = '/io/github/fqazzazee/Shuttle';
const INTERFACE = 'io.github.fqazzazee.Shuttle1';

export const Daemon = GObject.registerClass({
    Signals: {
        // running, state, paused or the rates changed.
        'changed': {},
        // A shuttle's status changed, or the shuttle is new.
        'shuttle-changed': {param_types: [GObject.TYPE_STRING]},
        'shuttle-removed': {param_types: [GObject.TYPE_STRING]},
        // The list of shuttles has arrived: no shuttles now means none.
        'listed': {},
        // The settings on disk are those the daemon runs: after any change.
        'config-reloaded': {},
    },
}, class Daemon extends GObject.Object {
    constructor() {
        super();
        // shuttles maps each shuttle's name to its status, the daemon's
        // a{sv} unpacked: see internal/bus/status.go.
        this.shuttles = new Map();
        this.listed = false;

        // No auto-start: the daemon is a systemd user service, and the
        // extension waits for it rather than starting it.
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
                // changed.
                // connect, not the Shell's connectObject: this runs in plain
                // gjs too.
                this._ids = [
                    this._proxy.connect('g-properties-changed', () => this.emit('changed')),
                    this._proxy.connect('notify::g-name-owner', () => this._ownerChanged()),
                    this._proxy.connect('g-signal', (_proxy, _sender, signal, params) => this._signal(signal, params)),
                ];
                this._ownerChanged();
            });
    }

    // running reports whether a daemon owns the bus name.
    get running() {
        return !!this._proxy?.g_name_owner;
    }

    _property(name) {
        return this.running ? this._proxy.get_cached_property(name)?.unpack() : null;
    }

    // state is the daemon's overall state, "" while it is not running.
    get state() {
        return this._property('State') ?? '';
    }

    // paused reports whether every shuttle is paused.
    get paused() {
        return this._property('Paused') ?? false;
    }

    get rateUp() {
        return this._property('RateUp') ?? 0;
    }

    get rateDown() {
        return this._property('RateDown') ?? 0;
    }

    // syncNow asks for a run of the named shuttle now.
    syncNow(name) {
        return this._call('SyncNow', new GLib.Variant('(s)', [name]));
    }

    // setPaused pauses or resumes the named shuttle, or every shuttle for "".
    setPaused(name, paused) {
        return this._call('SetPaused', new GLib.Variant('(sb)', [name, paused]));
    }

    // shuttleSettings are the named shuttle's settings as the rows of its
    // editor (internal/bus: ShuttleSettings): objects of key, label, hint,
    // kind, choices, value, inherited, from and off.
    async shuttleSettings(name) {
        const reply = await this._call('ShuttleSettings', new GLib.Variant('(s)', [name]));
        const [rows] = reply.recursiveUnpack();
        return rows.map(([key, label, hint, kind, choices, value, inherited, from, off]) =>
            ({key, label, hint, kind, choices, value, inherited, from, off}));
    }

    // setShuttleSetting sets one of the named shuttle's settings, as typed in
    // its row; "" removes the shuttle's own, so its profile's or the default
    // applies again.
    setShuttleSetting(name, key, value) {
        return this._call('SetShuttleSetting', new GLib.Variant('(sss)', [name, key, value]));
    }

    // _call calls a method of the daemon, and resolves with its reply. The
    // promise fails with the daemon's own words, e.g. that the shuttle is
    // turned off.
    _call(method, params) {
        return new Promise((resolve, reject) => {
            if (!this.running) {
                reject(new Error('the shuttle service is not running'));
                return;
            }
            this._proxy.call(method, params, Gio.DBusCallFlags.NONE, -1, this._cancellable, (proxy, result) => {
                try {
                    resolve(proxy.call_finish(result));
                } catch (e) {
                    Gio.DBusError.strip_remote_error(e);
                    reject(e);
                }
            });
        });
    }

    _ownerChanged() {
        for (const name of [...this.shuttles.keys()])
            this._remove(name);
        this.listed = false;
        this.emit('changed');
        if (this.running)
            this._list();
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
            if (!this.running)
                return;
            for (const [name, st] of Object.entries(all))
                this._set(name, st);
            for (const name of [...this.shuttles.keys()]) {
                if (!Object.hasOwn(all, name))
                    this._remove(name);
            }
            this.listed = true;
            this.emit('listed');
        });
    }

    _signal(signal, params) {
        if (signal === 'ShuttleChanged') {
            const [name, st] = params.recursiveUnpack();
            this._set(name, st);
        } else if (signal === 'ShuttleRemoved') {
            const [name] = params.recursiveUnpack();
            this._remove(name);
        } else if (signal === 'ConfigReloaded') {
            this.emit('config-reloaded');
        }
    }

    _set(name, st) {
        this.shuttles.set(name, st);
        this.emit('shuttle-changed', name);
    }

    _remove(name) {
        if (this.shuttles.delete(name))
            this.emit('shuttle-removed', name);
    }

    destroy() {
        this._cancellable.cancel();
        this._ids?.forEach(id => this._proxy.disconnect(id));
        this._proxy = null;
    }
});

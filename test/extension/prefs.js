// SPDX-License-Identifier: GPL-2.0-or-later

// The preferences' pages (extension/settingsPage.js), built on the headless
// shell's display against the running daemon, and used as a user would:
// open a shuttle, choose a value, type one, type one the config refuses,
// and set them back, reading config.toml after each.
//
//     gjs -m test/extension/prefs.js <config.toml> [<screenshot.png>]
//
// Run by inside.sh. It exits 1 with what went wrong.

import Adw from 'gi://Adw?version=1';
import GLib from 'gi://GLib';
import Gtk from 'gi://Gtk?version=4.0';
import System from 'system';

import {Daemon} from '../../extension/daemon.js';
import {fillWindow, shuttlePage} from '../../extension/settingsPage.js';

const [configPath, shot] = ARGV;
const fail = why => {
    printerr(`FAIL: ${why}`);
    System.exit(1);
};
const config = () => new TextDecoder().decode(GLib.file_get_contents(configPath)[1]);
// docs' own table: up to the next one.
const docs = () => config().split('[shuttles.docs]')[1]?.split('\n[')[0] ?? '';

const tick = ms => new Promise(resolve => GLib.timeout_add(GLib.PRIORITY_DEFAULT, ms, () => {
    resolve();
    return GLib.SOURCE_REMOVE;
}));
async function until(what, ok, seconds = 10) {
    for (let i = 0; i < seconds * 20; i++) {
        if (ok())
            return;
        await tick(50);
    }
    fail(`never: ${what}\n--- docs:\n${docs()}`);
}
// find is the first widget under root that ok accepts.
function find(root, ok) {
    for (let w = root.get_first_child(); w; w = w.get_next_sibling()) {
        if (ok(w))
            return w;
        const inner = find(w, ok);
        if (inner)
            return inner;
    }
    return null;
}

// screenshot draws the window as it is into shot, a PNG, when asked for.
function screenshot(window) {
    if (!shot)
        return;
    const paintable = new Gtk.WidgetPaintable({widget: window});
    const snapshot = Gtk.Snapshot.new();
    paintable.snapshot(snapshot, window.get_width(), window.get_height());
    const texture = window.get_native().get_renderer().render_texture(snapshot.to_node(), null);
    texture.save_to_png(shot);
}

Adw.init();
const daemon = new Daemon();
await until('the daemon listed its shuttles', () => daemon.listed);
const window = new Adw.PreferencesWindow({default_width: 760, default_height: 980});
fillWindow(window, daemon);
window.present();
await until('the list of shuttles', () => find(window, w => w instanceof Adw.ActionRow && w.title === 'docs'));

const page = shuttlePage(window, daemon, 'docs');
window.push_subpage(page);
await page.load();
const row = key => find(page, w => w.key === key) ?? fail(`no row ${key}`);

// A choice: run, as the shuttle's own; then not set again.
const metered = row('metered').dropdown;
if (metered.selected !== 0 || metered.model.get_string(0) !== 'pause (default)')
    fail(`metered at first: ${metered.selected}, ${metered.model.get_string(0)}`);
metered.selected = 2;
await until('metered = "run" in docs', () => docs().includes('metered = "run"'));

// Typed: the upload limit, which docs sets itself, changed; then cleared
// with its reset button.
const up = row('bandwidth.up');
if (up.entry.text !== '64KiB/s')
    fail(`bandwidth.up shows ${up.entry.text}, want docs' own 64KiB/s`);
up.entry.text = '1MB/s';
up.entry.emit('activate');
await until('bandwidth up 1MB/s', () => docs().includes('bandwidth = { up = "1MB/s" }'));

// One the config refuses: the file stays as it was, the entry as on disk.
const down = row('bandwidth.down');
const before = config();
down.entry.text = 'fast';
down.entry.emit('activate');
await until('the refused value cleared from the entry', () => down.entry.text === '');
if (config() !== before)
    fail('a refused value changed config.toml');

// A row that does not apply to a push shuttle is greyed, with why.
const hold = row('conflict_hold');
if (hold.sensitive || !hold.subtitle.startsWith('Does not apply'))
    fail(`conflict_hold for keep-both conflicts: sensitive ${hold.sensitive}, "${hold.subtitle}"`);

await tick(300); // drawn
screenshot(window);

metered.selected = 0;
await until('metered gone from docs', () => !docs().includes('metered'));
find(up, w => w instanceof Gtk.Button).emit('clicked');
await until('bandwidth back to none of docs\' own', () => !docs().includes('bandwidth'));

window.close();
daemon.destroy();
print('preferences: a choice, a typed value, a refused one, a greyed row, and resets: as on disk');
System.exit(0);

// SPDX-License-Identifier: GPL-2.0-or-later

// A shuttle's settings as the preferences window shows them: grouped, each
// saying what applies when the shuttle sets none, and which way to choose.
// A row is the daemon's (internal/bus: ShuttleSettings), as Daemon unpacks
// it. Nothing here imports GTK or GNOME Shell.

// SECTIONS group the rows, a key or the start of one each; any other key
// goes under Other, so a setting the window does not know yet still shows.
export const SECTIONS = [
    {title: 'Runs', keys: ['mode', 'triggers', 'interval', 'at', 'poll_interval']},
    {title: 'Conflicts and deletions', keys: ['conflicts', 'conflict_hold', 'deletions', 'deletion_guard']},
    {title: 'Versions', keys: ['versioning.']},
    {title: 'Network', keys: ['metered', 'bandwidth.']},
    {title: 'Left out', keys: ['ignore']},
    {title: 'Notifications', keys: ['notify.']},
];

const inSection = (s, key) => s.keys.some(k => k.endsWith('.') ? key.startsWith(k) : key === k);

// sections are the rows in SECTIONS's groups, in the daemon's order within
// each; groups without a row are left out.
export function sections(rows) {
    const out = SECTIONS.map(s => ({title: s.title, rows: rows.filter(r => inSection(s, r.key))}));
    out.push({title: 'Other', rows: rows.filter(r => !SECTIONS.some(s => inSection(s, r.key)))});
    return out.filter(s => s.rows.length > 0);
}

// inheritedText is what applies when the shuttle sets none, and from where,
// short enough to show in a drop-down: "pause (default)", "run (profile
// live)". The TUI's "-" for nothing is "none".
export function inheritedText(row) {
    const value = row.inherited === '-' || row.inherited === '' ? 'none' : row.inherited;
    return `${value} (${row.from})`;
}

// choiceItems are a choice row's options: what applies when not set, then
// each choice.
export function choiceItems(row) {
    return [inheritedText(row), ...row.choices];
}

// reason is an error from the daemon as a person reads it: without the
// position in config.toml the parser starts it with.
export function reason(message) {
    return message.replace(/^line \d+, column \d+: (toml: )?/, '');
}

// selectedIndex is which of choiceItems the shuttle's own value is; 0 for
// none, or one the window does not know.
export function selectedIndex(row) {
    return row.choices.indexOf(row.value) + 1;
}

// valueAt is the value choosing choiceItems' index sets: "" for not set.
export function valueAt(row, index) {
    return index > 0 ? row.choices[index - 1] ?? '' : '';
}

// subtitle is what a row says under its title: why it does not apply, or
// its hint.
export function subtitle(row) {
    if (row.off)
        return `Does not apply: ${row.off}`;
    return row.hint ? row.hint[0].toUpperCase() + row.hint.slice(1) : '';
}

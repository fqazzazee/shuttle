// SPDX-License-Identifier: GPL-2.0-or-later

// A shuttle's status in words, as the TUI and the command line put it
// (internal/present). A status is the daemon's a{sv}, unpacked: see
// internal/bus/status.go. Nothing here imports GNOME Shell.

import {size} from './format.js';

// modeArrow is a mode as an arrow: ↑ push, ↓ pull, ⇅ sync, as the TUI has
// them.
export function modeArrow(mode) {
    return {push: '↑', pull: '↓', sync: '⇅'}[mode] ?? '?';
}

// plural is n things: "1 file", "3 files".
export function plural(n, thing) {
    return n === 1 ? `1 ${thing}` : `${n} ${thing}s`;
}

// ago is a short "how long ago" of a unix time in seconds, at now (ms):
// "just now", "5m ago", "3h ago", "2d ago"; "never" for 0.
export function ago(unix, now) {
    if (!unix)
        return 'never';
    const s = Math.floor(now / 1000) - unix;
    if (s < 60)
        return 'just now';
    if (s < 3600)
        return `${Math.floor(s / 60)}m ago`;
    if (s < 86400)
        return `${Math.floor(s / 3600)}h ago`;
    return `${Math.floor(s / 86400)}d ago`;
}

// heldConflicts counts the conflicts waiting for the user's choice. Each
// conflict is (path, reason, outcome, local copy, remote copy).
export function heldConflicts(st) {
    return (st.Conflicts ?? []).filter(c => c[2] === 'held').length;
}

// attention lists what needs the user, most pressing first: "2 conflicts
// waiting for you; a run is waiting".
export function attention(st) {
    const parts = [];
    if (st.Error)
        parts.push(st.Error);
    const held = heldConflicts(st);
    if (held > 0)
        parts.push(`${plural(held, 'conflict')} waiting for you`);
    if (st.HeldDeletions > 0)
        parts.push(`${plural(st.HeldDeletions, 'deletion')} held by the guard`);
    if (st.Pending)
        parts.push('a run is waiting');
    if (st.Note)
        parts.push(st.Note);
    return parts.join('; ');
}

// progress describes a run in progress: "syncing 12 of 30, 45.0 MB of
// 120.0 MB, ↑ 2.1 MB/s: docs/a.pdf"; "" when not running.
export function progress(st) {
    let s;
    switch (st.Phase ?? '') {
    case '':
        return '';
    case 'scanning':
        s = `scanning, ${st.Done} found`;
        break;
    case 'comparing':
        s = `comparing ${st.Done} of ${st.Total}`;
        break;
    default:
        s = `${st.Phase} ${st.Done} of ${st.Total}`;
        if (st.TotalBytes > 0)
            s += `, ${size(st.Bytes)} of ${size(st.TotalBytes)}`;
    }
    if (st.RateUp > 0)
        s += `, ↑ ${size(st.RateUp)}/s`;
    if (st.RateDown > 0)
        s += `, ↓ ${size(st.RateDown)}/s`;
    if (st.Path)
        s += `: ${st.Path}`;
    return s;
}

// row is what the dropdown shows of one shuttle: its name and mode, when it
// last ran, and a line on what it is doing or what needs the user, ""
// when nothing does.
export function row(name, st, now) {
    return {
        name,
        state: st.State ?? '',
        title: `${name}  ${modeArrow(st.Mode)}`,
        when: st.Phase ? 'now' : ago(st.LastRun, now),
        detail: [progress(st), attention(st)].filter(Boolean).join('; '),
    };
}

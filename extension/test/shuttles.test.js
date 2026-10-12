// SPDX-License-Identifier: GPL-2.0-or-later

import {ago, attention, heldConflicts, modeArrow, progress, row} from '../lib/shuttles.js';
import {equal, test} from './t.js';

// The cases below are internal/present's, so the dropdown, the TUI and
// `shuttle status` say things the same way.

const now = Date.UTC(2026, 9, 11, 12); // ms
const unix = s => Math.floor(now / 1000) - s;

test('ago', () => {
    equal(ago(unix(10), now), 'just now');
    equal(ago(unix(5 * 60), now), '5m ago');
    equal(ago(unix(3 * 3600), now), '3h ago');
    equal(ago(unix(50 * 3600), now), '2d ago');
    equal(ago(0, now), 'never');
});

test('progress', () => {
    equal(progress({}), '', 'not running');
    equal(progress({Phase: 'scanning', Done: 1234}), 'scanning, 1234 found');
    equal(progress({Phase: 'comparing', Done: 3, Total: 10, RateDown: 999, Path: 'a.txt'}),
        'comparing 3 of 10, ↓ 999 B/s: a.txt');
    equal(progress({Phase: 'syncing', Done: 12, Total: 30, Bytes: 45_000_000, TotalBytes: 120_000_000,
        RateUp: 2_100_000, Path: 'docs/a.pdf'}), 'syncing 12 of 30, 45.0 MB of 120.0 MB, ↑ 2.1 MB/s: docs/a.pdf');
    equal(progress({Phase: 'syncing', Done: 1, Total: 2}), 'syncing 1 of 2');
});

test('attention, most pressing first', () => {
    const conflict = outcome => ['a.txt', 'both changed', outcome, ['file', 1, 0], ['file', 2, 0]];
    const st = {
        Error: 'share disk: connection refused', Conflicts: [conflict('held'), conflict('held'), conflict('kept both')],
        HeldDeletions: 1, Pending: true, Note: 'the share came back',
    };
    equal(heldConflicts(st), 2, 'held conflicts');
    equal(attention(st), 'share disk: connection refused; 2 conflicts waiting for you; ' +
        '1 deletion held by the guard; a run is waiting; the share came back');
    equal(attention({}), '', 'nothing');
});

test('mode arrows', () => {
    equal(modeArrow('push'), '→');
    equal(modeArrow('pull'), '←');
    equal(modeArrow('sync'), '↔');
    equal(modeArrow(''), '?');
});

test('a row: name, when, and what is going on', () => {
    const idle = row('docs', {State: 'idle', Mode: 'push', LastRun: unix(300)}, now);
    equal(idle.title, 'docs  →', 'title');
    equal(idle.when, '5m ago', 'when');
    equal(idle.detail, '', 'detail');
    const busy = row('docs', {State: 'running', Mode: 'sync', LastRun: unix(300), Phase: 'syncing', Done: 1, Total: 2,
        Pending: true}, now);
    equal(busy.when, 'now', 'when, running');
    equal(busy.detail, 'syncing 1 of 2; a run is waiting', 'detail, running');
});

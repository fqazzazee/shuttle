// SPDX-License-Identifier: GPL-2.0-or-later

import {NOT_RUNNING, known, presentation} from '../lib/state.js';
import {equal, ok, readRepoFile, test} from './t.js';

// A state the daemon learns must not reach the top bar as an unknown one:
// this reads the daemon's own list.
test('every state the daemon has is known', () => {
    const go = readRepoFile('internal/daemon/status.go');
    const states = [...go.matchAll(/^\s*State\w+\s+State\s*=\s*"([a-z-]+)"/gm)].map(m => m[1]);
    ok(states.length >= 7, `found only ${states.length} states in status.go: has the file changed shape?`);
    for (const s of states)
        ok(known(s), `the daemon's state "${s}" has no presentation in lib/state.js`);
});

test('the service not running has its own presentation', () => {
    ok(known(NOT_RUNNING), 'NOT_RUNNING is not known');
    ok(presentation(NOT_RUNNING).label.includes('not running'), presentation(NOT_RUNNING).label);
});

test('an unknown state still shows, as itself', () => {
    const p = presentation('sideways');
    equal(p.icon, 'dialog-error-symbolic', 'icon');
    ok(p.label.includes('sideways'), p.label);
});

test('problems look different from being in step', () => {
    const fine = presentation('idle').icon;
    for (const s of ['paused', 'held', 'error', 'offline', NOT_RUNNING])
        ok(presentation(s).icon !== fine, `${s} looks like idle`);
});

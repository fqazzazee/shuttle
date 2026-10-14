// SPDX-License-Identifier: GPL-2.0-or-later

import {choiceItems, inheritedText, reason, sections, selectedIndex, subtitle, valueAt} from '../lib/settings.js';
import {equal, test} from './t.js';

// Rows as the daemon sends them (internal/bus: ShuttleSettings).
const row = (key, extra = {}) => ({
    key, label: key, hint: 'a hint', kind: 'text', choices: [], value: '', inherited: '', from: 'default', off: '',
    ...extra,
});
const metered = row('metered', {kind: 'choice', choices: ['pause', 'run'], inherited: 'run', from: 'profile live'});

test('sections: grouped, in order, others last, empty ones left out', () => {
    const got = sections([row('mode'), row('metered'), row('bandwidth.up'), row('notify.errors'), row('something new')]);
    equal(got.map(s => s.title).join('|'), 'Runs|Network|Notifications|Other');
    equal(got[1].rows.map(r => r.key).join('|'), 'metered|bandwidth.up', 'a group key by its start');
});

test('what applies when not set, and from where', () => {
    equal(inheritedText(metered), 'run (profile live)');
    equal(inheritedText(row('conflicts', {inherited: 'keep-both'})), 'keep-both (default)');
    equal(inheritedText(row('interval', {inherited: '-'})), 'none (default)', "the TUI's - for nothing");
});

test('a choice: not set first, then the choices', () => {
    equal(choiceItems(metered).join('|'), 'run (profile live)|pause|run');
    equal(selectedIndex(metered), 0, 'not set');
    equal(selectedIndex({...metered, value: 'run'}), 2, 'run');
    equal(selectedIndex({...metered, value: 'sometimes'}), 0, 'a value it does not know: not set');
    equal(valueAt(metered, 0), '', 'not set');
    equal(valueAt(metered, 1), 'pause');
    equal(valueAt(metered, 9), '', 'out of range');
});

test('an error as a person reads it', () => {
    equal(reason('line 9, column 36: toml: invalid rate "fast": use a size per second, e.g. 20MB/s'),
        'invalid rate "fast": use a size per second, e.g. 20MB/s');
    equal(reason('Mode: "both" is none of [push pull sync]'), 'Mode: "both" is none of [push pull sync]', 'nothing to take off');
});

test('subtitle: why a row does not apply, else its hint', () => {
    equal(subtitle(row('deletions', {off: 'only for push and pull'})), 'Does not apply: only for push and pull');
    equal(subtitle(row('ignore', {hint: 'patterns, e.g. *.tmp'})), 'Patterns, e.g. *.tmp');
    equal(subtitle(row('x', {hint: ''})), '');
});

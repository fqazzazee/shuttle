// SPDX-License-Identifier: GPL-2.0-or-later

import {rates, size} from '../lib/format.js';
import {equal, ok, readRepoFile, test} from './t.js';

// The top bar and the TUI write sizes alike: these are the TUI's own cases.
test('sizes match the TUI', () => {
    const go = readRepoFile('internal/present/present_test.go');
    const table = go.match(/for n, want := range map\[int64\]string\{([^}]*)\}/);
    ok(table, 'no Size table in present_test.go: has the file changed shape?');
    const cases = [...table[1].matchAll(/([\d_]+):\s*"([^"]+)"/g)];
    ok(cases.length >= 5, `found only ${cases.length} cases`);
    for (const [, n, want] of cases)
        equal(size(Number(n.replaceAll('_', ''))), want, `size(${n})`);
});

test('rates show only what moves', () => {
    equal(rates(0, 0), '', 'nothing');
    equal(rates(2_100_000, 0), '↑ 2.1 MB/s', 'up');
    equal(rates(0, 999), '↓ 999 B/s', 'down');
    equal(rates(2_100_000, 300_000), '↑ 2.1 MB/s ↓ 300.0 kB/s', 'both');
});

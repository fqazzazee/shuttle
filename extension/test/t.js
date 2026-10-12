// SPDX-License-Identifier: GPL-2.0-or-later

// The few helpers the unit tests need: see run.js.

import Gio from 'gi://Gio';

export const tests = [];

export function test(name, fn) {
    tests.push({name, fn});
}

export function equal(got, want, what = 'value') {
    if (got !== want)
        throw new Error(`${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
}

export function ok(cond, message) {
    if (!cond)
        throw new Error(message);
}

// readRepoFile reads a file of this repository, by its path from the top.
export function readRepoFile(path) {
    const top = Gio.File.new_for_uri(import.meta.url).get_parent().get_parent().get_parent();
    const [, bytes] = top.resolve_relative_path(path).load_contents(null);
    return new TextDecoder().decode(bytes);
}

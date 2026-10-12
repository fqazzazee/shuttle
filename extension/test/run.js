// SPDX-License-Identifier: GPL-2.0-or-later

// Runs the extension's unit tests in plain gjs, without GNOME Shell:
//
//     gjs -m extension/test/run.js
//
// Each *.test.js beside this file registers tests with test(); a test
// fails by throwing. The modules under test import nothing from the Shell.

import Gio from 'gi://Gio';
import System from 'system';

import {tests} from './t.js';

const here = Gio.File.new_for_uri(import.meta.url).get_parent();
const files = [];
const children = here.enumerate_children('standard::name', Gio.FileQueryInfoFlags.NONE, null);
for (let info; (info = children.next_file(null));) {
    if (info.get_name().endsWith('.test.js'))
        files.push(info.get_name());
}
for (const name of files.sort())
    await import(here.get_child(name).get_uri());

let failed = 0;
for (const {name, fn} of tests) {
    try {
        fn();
        print(`ok    ${name}`);
    } catch (e) {
        failed++;
        print(`FAIL  ${name}\n      ${e.message}`);
    }
}
print(`${tests.length - failed} passed, ${failed} failed`);
System.exit(failed || tests.length === 0 ? 1 : 0);

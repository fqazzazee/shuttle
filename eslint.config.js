// SPDX-License-Identifier: GPL-2.0-or-later

// Lint for the GNOME extension (`make lint-extension`): correctness, and the
// few rules the code is already written to. Formatting rules have left
// eslint's core; none are checked.

import js from '@eslint/js';

// What gjs and GNOME Shell provide besides the language. global is the
// Shell's; the unit tests run in plain gjs, which has the rest, and ARGV
// for a script's arguments.
const gjs = {
    ARGV: 'readonly',
    console: 'readonly',
    global: 'readonly',
    log: 'readonly',
    logError: 'readonly',
    print: 'readonly',
    printerr: 'readonly',
    TextDecoder: 'readonly',
    TextEncoder: 'readonly',
};

export default [
    js.configs.recommended,
    {
        files: ['extension/**/*.js', 'test/extension/**/*.js'],
        languageOptions: {
            ecmaVersion: 2024,
            sourceType: 'module',
            globals: gjs,
        },
        rules: {
            // GNOME's style: braces only where a body takes more than a line.
            'curly': ['error', 'multi-or-nest', 'consistent'],
            'eqeqeq': 'error',
            'no-unused-vars': ['error', {argsIgnorePattern: '^_'}],
            'no-var': 'error',
            'prefer-const': 'error',
        },
    },
];

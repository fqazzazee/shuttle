// SPDX-License-Identifier: GPL-2.0-or-later

// Numbers as the TUI writes them (internal/present). Nothing here imports
// GNOME Shell.

// size is a byte count in decimal units, as GNOME shows them: "2.1 MB".
export function size(n) {
    if (n < 1000)
        return `${n} B`;
    let f = n / 1000, unit = 'kB';
    for (const u of ['MB', 'GB', 'TB']) {
        if (f < 1000)
            break;
        f /= 1000;
        unit = u;
    }
    return `${f.toFixed(1)} ${unit}`;
}

// rates is the transfer rates for the top bar, "↑ 2.1 MB/s ↓ 300.0 kB/s",
// each only while it is above zero; "" when nothing moves.
export function rates(up, down) {
    const parts = [];
    if (up > 0)
        parts.push(`↑ ${size(up)}/s`);
    if (down > 0)
        parts.push(`↓ ${size(down)}/s`);
    return parts.join(' ');
}

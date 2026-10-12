// SPDX-License-Identifier: GPL-2.0-or-later

// What the indicator shows for the daemon's state. Nothing here imports
// GNOME Shell, so the unit tests run it in plain gjs.

// SHUTTLE_ICON stands for the extension's own icon, icons/shuttle-symbolic.svg:
// the icon theme has nothing for "in step" or "syncing".
export const SHUTTLE_ICON = 'shuttle';

// NOT_RUNNING is the state shown while no daemon owns the bus name.
export const NOT_RUNNING = 'not-running';

// The daemon's states (internal/daemon/status.go), and NOT_RUNNING.
const STATES = {
    'idle': {icon: SHUTTLE_ICON, label: 'Up to date'},
    'running': {icon: SHUTTLE_ICON, label: 'Syncing'},
    'paused': {icon: 'media-playback-pause-symbolic', label: 'Paused'},
    'held': {icon: 'dialog-warning-symbolic', label: 'Waiting for you'},
    'error': {icon: 'dialog-error-symbolic', label: 'A shuttle failed'},
    'offline': {icon: 'network-offline-symbolic', label: 'A share is offline'},
    'disabled': {icon: 'action-unavailable-symbolic', label: 'Turned off'},
    [NOT_RUNNING]: {icon: 'action-unavailable-symbolic', label: 'The shuttle service is not running'},
};

// presentation returns the icon and label for state. A state this version
// does not know, from a newer daemon, still shows: as itself, with the
// error icon, rather than as nothing.
export function presentation(state) {
    return STATES[state] ?? {icon: 'dialog-error-symbolic', label: `Unknown state "${state}"`};
}

// known reports whether state has its own presentation.
export function known(state) {
    return Object.hasOwn(STATES, state);
}

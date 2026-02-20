// Global keyboard shortcuts — central registry for application-wide keybindings

import { focusDrawerSearch } from './system-drawer.ts';
import { toggleConfig } from './config-panel.ts';

/** Check if an input element is focused (skip global shortcuts when typing) */
export function isInputFocused(target: EventTarget | null): boolean {
    if (!target || !(target instanceof HTMLElement)) return false;
    return target.tagName === 'INPUT'
        || target.tagName === 'TEXTAREA'
        || target.isContentEditable
        || target.closest('.cm-editor') !== null;
}

/** Register global keyboard shortcuts */
export function initGlobalKeyboard(): void {
    document.addEventListener('keydown', (e: KeyboardEvent) => {
        // Cmd+, on Mac, Ctrl+, on Windows/Linux — toggle config panel
        if ((e.metaKey || e.ctrlKey) && e.key === ',') {
            e.preventDefault();
            toggleConfig();
            return;
        }

        // SPACE opens unified search when nothing is focused
        if (e.key === ' ' && !isInputFocused(e.target)) {
            e.preventDefault();
            focusDrawerSearch();
        }
    });
}

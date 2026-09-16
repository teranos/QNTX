// Global keyboard shortcuts — central registry for application-wide keybindings

import { focusDrawerSearch } from './system-drawer.ts';

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
        // Suppress Tab — QNTX uses hjkl for navigation, Tab's browser focus cycling is unwanted
        if (e.key === 'Tab' && !isInputFocused(e.target)) {
            e.preventDefault();
            return;
        }

        // SPACE opens unified search when nothing is focused. A shut door holds
        // the whole bar, and search behind it reaches an app nobody is in yet.
        if (e.key === ' ' && !isInputFocused(e.target)) {
            e.preventDefault();
            if (document.getElementById('system-drawer')?.classList.contains('door-held')) {
                return;
            }
            focusDrawerSearch();
        }
    });
}

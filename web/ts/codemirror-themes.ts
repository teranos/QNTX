/**
 * CodeMirror 6 Theme System
 *
 * Provides light and dark themes for all CodeMirror editors in the application.
 * Themes react to the `data-theme` attribute on the body element.
 */

import { EditorView } from '@codemirror/view';
import { Extension } from '@codemirror/state';
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { tags } from '@lezer/highlight';

// Theme type
export type ThemeMode = 'light' | 'dark';

/**
 * Get current theme mode from document
 */
export function getCurrentTheme(): ThemeMode {
    const theme = document.body.dataset.theme;
    return theme === 'light' ? 'light' : 'dark';
}

// ============================================================================
// Dark Theme
// ============================================================================

const darkColors = {
    background: '#1e1e1e',
    foreground: '#d4d4d4',
    caret: '#4ade80',
    selection: 'rgba(74, 222, 128, 0.2)',
    selectionMatch: 'rgba(74, 222, 128, 0.1)',
    gutterBackground: 'transparent',
    gutterForeground: '#555',
    gutterBorder: '#333',
    activeLineBackground: 'rgba(74, 222, 128, 0.03)',
    activeLineGutterBackground: 'rgba(74, 222, 128, 0.05)',
    matchingBracket: 'rgba(74, 222, 128, 0.2)',
    matchingBracketOutline: 'rgba(74, 222, 128, 0.5)',
    tooltipBackground: '#2a2a2a',
    tooltipBorder: '#444',
};

const darkEditorTheme = EditorView.theme({
    '&': {
        backgroundColor: darkColors.background,
        color: darkColors.foreground,
    },
    '.cm-content': {
        caretColor: darkColors.caret,
        padding: '8px 0',
    },
    '.cm-cursor, .cm-dropCursor': {
        borderLeftColor: darkColors.caret,
    },
    '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
        backgroundColor: darkColors.selection,
    },
    '.cm-selectionMatch': {
        backgroundColor: darkColors.selectionMatch,
    },
    '.cm-gutters': {
        backgroundColor: darkColors.gutterBackground,
        borderRight: `1px solid ${darkColors.gutterBorder}`,
        color: darkColors.gutterForeground,
    },
    '.cm-activeLineGutter': {
        backgroundColor: darkColors.activeLineGutterBackground,
    },
    '.cm-activeLine': {
        backgroundColor: darkColors.activeLineBackground,
    },
    '.cm-matchingBracket': {
        backgroundColor: darkColors.matchingBracket,
        outline: `1px solid ${darkColors.matchingBracketOutline}`,
    },
    '.cm-nonmatchingBracket': {
        backgroundColor: 'rgba(239, 68, 68, 0.2)',
        outline: '1px solid rgba(239, 68, 68, 0.5)',
    },
    '.cm-tooltip': {
        backgroundColor: darkColors.tooltipBackground,
        border: `1px solid ${darkColors.tooltipBorder}`,
    },
    '.cm-tooltip-autocomplete': {
        '& > ul': {
            fontFamily: 'inherit',
        },
        '& > ul > li': {
            padding: '4px 8px',
            color: '#ccc',
        },
        '& > ul > li[aria-selected]': {
            backgroundColor: darkColors.selection,
            color: '#fff',
        },
    },
}, { dark: true });

const darkHighlightStyle = HighlightStyle.define([
    { tag: tags.keyword, color: '#c586c0' },
    { tag: tags.operator, color: '#d4d4d4' },
    { tag: tags.special(tags.variableName), color: '#9cdcfe' },
    { tag: tags.typeName, color: '#4ec9b0' },
    { tag: tags.atom, color: '#569cd6' },
    { tag: tags.number, color: '#b5cea8' },
    { tag: tags.definition(tags.variableName), color: '#9cdcfe' },
    { tag: tags.string, color: '#ce9178' },
    { tag: tags.special(tags.string), color: '#d7ba7d' },
    { tag: tags.comment, color: '#6a9955' },
    { tag: tags.variableName, color: '#9cdcfe' },
    { tag: tags.tagName, color: '#569cd6' },
    { tag: tags.bracket, color: '#d4d4d4' },
    { tag: tags.meta, color: '#d4d4d4' },
    { tag: tags.link, color: '#569cd6', textDecoration: 'underline' },
    { tag: tags.heading, color: '#569cd6', fontWeight: 'bold' },
    { tag: tags.emphasis, fontStyle: 'italic' },
    { tag: tags.strong, fontWeight: 'bold' },
    { tag: tags.strikethrough, textDecoration: 'line-through' },
    { tag: tags.className, color: '#4ec9b0' },
    { tag: tags.propertyName, color: '#9cdcfe' },
    { tag: tags.function(tags.variableName), color: '#dcdcaa' },
    { tag: tags.function(tags.propertyName), color: '#dcdcaa' },
    { tag: tags.bool, color: '#569cd6' },
    { tag: tags.null, color: '#569cd6' },
    { tag: tags.regexp, color: '#d16969' },
    { tag: tags.escape, color: '#d7ba7d' },
]);

// ============================================================================
// Light Theme
// ============================================================================

const lightColors = {
    background: '#ffffff',
    foreground: '#1e1e1e',
    caret: '#16a34a',
    selection: 'rgba(22, 163, 74, 0.2)',
    selectionMatch: 'rgba(22, 163, 74, 0.1)',
    gutterBackground: '#fafafa',
    gutterForeground: '#999',
    gutterBorder: '#e0e0e0',
    activeLineBackground: 'rgba(22, 163, 74, 0.05)',
    activeLineGutterBackground: 'rgba(22, 163, 74, 0.08)',
    matchingBracket: 'rgba(22, 163, 74, 0.25)',
    matchingBracketOutline: 'rgba(22, 163, 74, 0.6)',
    tooltipBackground: '#ffffff',
    tooltipBorder: '#d4d4d4',
};

const lightEditorTheme = EditorView.theme({
    '&': {
        backgroundColor: lightColors.background,
        color: lightColors.foreground,
    },
    '.cm-content': {
        caretColor: lightColors.caret,
        padding: '8px 0',
    },
    '.cm-cursor, .cm-dropCursor': {
        borderLeftColor: lightColors.caret,
    },
    '&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
        backgroundColor: lightColors.selection,
    },
    '.cm-selectionMatch': {
        backgroundColor: lightColors.selectionMatch,
    },
    '.cm-gutters': {
        backgroundColor: lightColors.gutterBackground,
        borderRight: `1px solid ${lightColors.gutterBorder}`,
        color: lightColors.gutterForeground,
    },
    '.cm-activeLineGutter': {
        backgroundColor: lightColors.activeLineGutterBackground,
    },
    '.cm-activeLine': {
        backgroundColor: lightColors.activeLineBackground,
    },
    '.cm-matchingBracket': {
        backgroundColor: lightColors.matchingBracket,
        outline: `1px solid ${lightColors.matchingBracketOutline}`,
    },
    '.cm-nonmatchingBracket': {
        backgroundColor: 'rgba(220, 38, 38, 0.15)',
        outline: '1px solid rgba(220, 38, 38, 0.4)',
    },
    '.cm-tooltip': {
        backgroundColor: lightColors.tooltipBackground,
        border: `1px solid ${lightColors.tooltipBorder}`,
    },
    '.cm-tooltip-autocomplete': {
        '& > ul': {
            fontFamily: 'inherit',
        },
        '& > ul > li': {
            padding: '4px 8px',
            color: '#333',
        },
        '& > ul > li[aria-selected]': {
            backgroundColor: lightColors.selection,
            color: '#000',
        },
    },
}, { dark: false });

const lightHighlightStyle = HighlightStyle.define([
    { tag: tags.keyword, color: '#af00db' },
    { tag: tags.operator, color: '#1e1e1e' },
    { tag: tags.special(tags.variableName), color: '#001080' },
    { tag: tags.typeName, color: '#267f99' },
    { tag: tags.atom, color: '#0000ff' },
    { tag: tags.number, color: '#098658' },
    { tag: tags.definition(tags.variableName), color: '#001080' },
    { tag: tags.string, color: '#a31515' },
    { tag: tags.special(tags.string), color: '#795e26' },
    { tag: tags.comment, color: '#008000' },
    { tag: tags.variableName, color: '#001080' },
    { tag: tags.tagName, color: '#0000ff' },
    { tag: tags.bracket, color: '#1e1e1e' },
    { tag: tags.meta, color: '#1e1e1e' },
    { tag: tags.link, color: '#0000ff', textDecoration: 'underline' },
    { tag: tags.heading, color: '#0000ff', fontWeight: 'bold' },
    { tag: tags.emphasis, fontStyle: 'italic' },
    { tag: tags.strong, fontWeight: 'bold' },
    { tag: tags.strikethrough, textDecoration: 'line-through' },
    { tag: tags.className, color: '#267f99' },
    { tag: tags.propertyName, color: '#001080' },
    { tag: tags.function(tags.variableName), color: '#795e26' },
    { tag: tags.function(tags.propertyName), color: '#795e26' },
    { tag: tags.bool, color: '#0000ff' },
    { tag: tags.null, color: '#0000ff' },
    { tag: tags.regexp, color: '#811f3f' },
    { tag: tags.escape, color: '#795e26' },
]);

// ============================================================================
// Exported Themes
// ============================================================================

/**
 * Dark theme extension for CodeMirror
 */
export const darkTheme: Extension = [
    darkEditorTheme,
    syntaxHighlighting(darkHighlightStyle),
];

/**
 * Light theme extension for CodeMirror
 */
export const lightTheme: Extension = [
    lightEditorTheme,
    syntaxHighlighting(lightHighlightStyle),
];

/**
 * Get the CodeMirror theme extension based on current theme mode
 */
export function getTheme(mode?: ThemeMode): Extension {
    const themeMode = mode ?? getCurrentTheme();
    return themeMode === 'light' ? lightTheme : darkTheme;
}

/**
 * Get the theme extension for the current document theme
 */
export function getCurrentThemeExtension(): Extension {
    return getTheme(getCurrentTheme());
}

// ============================================================================
// ATS-specific Theme (for code blocks in prose editor)
// ============================================================================

/**
 * ATS Code Block dark theme - uses CSS variables for flexibility
 */
export const atsBlockDarkTheme = EditorView.theme({
    '&': {
        fontSize: 'var(--ats-editor-font-size, 14px)',
        fontFamily: "var(--ats-editor-font-family, 'JetBrains Mono', 'Fira Code', 'Consolas', monospace)",
        backgroundColor: 'var(--ats-editor-bg, #1a1a2e)',
    },
    '.cm-content': {
        caretColor: 'var(--ats-editor-caret-color, #66b3ff)',
        color: 'var(--ats-editor-text-color, #d4d4d4)',
        padding: 'var(--ats-editor-padding, 16px)',
    },
    '.cm-cursor, .cm-cursor-primary': {
        borderLeftColor: 'var(--ats-editor-caret-color, #66b3ff) !important',
        borderLeftWidth: 'var(--ats-editor-cursor-width, 3px) !important',
    },
    '.cm-line': {
        color: 'var(--ats-editor-text-color, #d4d4d4)',
    },
    '.cm-selectionBackground': {
        backgroundColor: 'var(--ats-editor-selection, rgba(102, 179, 255, 0.2)) !important',
    },
}, { dark: true });

/**
 * ATS Code Block light theme - uses CSS variables for flexibility
 */
export const atsBlockLightTheme = EditorView.theme({
    '&': {
        fontSize: 'var(--ats-editor-font-size, 14px)',
        fontFamily: "var(--ats-editor-font-family, 'JetBrains Mono', 'Fira Code', 'Consolas', monospace)",
        backgroundColor: 'var(--ats-editor-bg, #f8f9fa)',
    },
    '.cm-content': {
        caretColor: 'var(--ats-editor-caret-color, #16a34a)',
        color: 'var(--ats-editor-text-color, #1e1e1e)',
        padding: 'var(--ats-editor-padding, 16px)',
    },
    '.cm-cursor, .cm-cursor-primary': {
        borderLeftColor: 'var(--ats-editor-caret-color, #16a34a) !important',
        borderLeftWidth: 'var(--ats-editor-cursor-width, 3px) !important',
    },
    '.cm-line': {
        color: 'var(--ats-editor-text-color, #1e1e1e)',
    },
    '.cm-selectionBackground': {
        backgroundColor: 'var(--ats-editor-selection, rgba(22, 163, 74, 0.2)) !important',
    },
}, { dark: false });

/**
 * Get ATS block theme based on current theme mode
 */
export function getAtsBlockTheme(mode?: ThemeMode): Extension {
    const themeMode = mode ?? getCurrentTheme();
    return themeMode === 'light' ? atsBlockLightTheme : atsBlockDarkTheme;
}

// ============================================================================
// Go-specific Theme (for code blocks in prose editor)
// ============================================================================

/**
 * Go Code Block dark theme
 */
export const goBlockDarkTheme = EditorView.theme({
    '&': {
        fontSize: '14px',
        fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
        backgroundColor: '#1e1e1e',
    },
    '.cm-content': {
        caretColor: '#00add8',
        padding: '16px',
    },
    '.cm-cursor, .cm-cursor-primary': {
        borderLeftColor: '#00add8 !important',
        borderLeftWidth: '2px !important',
    },
    '.cm-gutters': {
        backgroundColor: '#1e1e1e',
        color: '#858585',
        border: 'none',
    },
    '.cm-activeLineGutter': {
        backgroundColor: '#2a2d2e',
    },
    '.cm-activeLine': {
        backgroundColor: 'rgba(255, 255, 255, 0.04)',
    },
}, { dark: true });

/**
 * Go Code Block light theme
 */
export const goBlockLightTheme = EditorView.theme({
    '&': {
        fontSize: '14px',
        fontFamily: "'JetBrains Mono', 'Fira Code', 'Consolas', monospace",
        backgroundColor: '#ffffff',
    },
    '.cm-content': {
        caretColor: '#00758f',
        padding: '16px',
    },
    '.cm-cursor, .cm-cursor-primary': {
        borderLeftColor: '#00758f !important',
        borderLeftWidth: '2px !important',
    },
    '.cm-gutters': {
        backgroundColor: '#fafafa',
        color: '#999',
        border: 'none',
        borderRight: '1px solid #e0e0e0',
    },
    '.cm-activeLineGutter': {
        backgroundColor: '#f0f0f0',
    },
    '.cm-activeLine': {
        backgroundColor: 'rgba(0, 0, 0, 0.03)',
    },
}, { dark: false });

/**
 * Get Go block theme based on current theme mode
 */
export function getGoBlockTheme(mode?: ThemeMode): Extension {
    const themeMode = mode ?? getCurrentTheme();
    return themeMode === 'light' ? goBlockLightTheme : goBlockDarkTheme;
}

// ============================================================================
// Theme Change Observer
// ============================================================================

type ThemeChangeCallback = (mode: ThemeMode) => void;
const themeChangeCallbacks: ThemeChangeCallback[] = [];

/**
 * Subscribe to theme changes
 * @param callback Function to call when theme changes
 * @returns Unsubscribe function
 */
export function onThemeChange(callback: ThemeChangeCallback): () => void {
    themeChangeCallbacks.push(callback);
    return () => {
        const index = themeChangeCallbacks.indexOf(callback);
        if (index > -1) {
            themeChangeCallbacks.splice(index, 1);
        }
    };
}

/**
 * Toggle between light and dark themes
 */
export function toggleTheme(): ThemeMode {
    const current = getCurrentTheme();
    const newTheme = current === 'dark' ? 'light' : 'dark';
    setTheme(newTheme);
    return newTheme;
}

/**
 * Set the theme explicitly
 */
export function setTheme(mode: ThemeMode): void {
    document.body.dataset.theme = mode;
    localStorage.setItem('qntx-theme', mode);

    // Notify subscribers
    themeChangeCallbacks.forEach(callback => callback(mode));
}

/**
 * Initialize theme from localStorage or system preference
 */
export function initTheme(): ThemeMode {
    // Check localStorage first
    const saved = localStorage.getItem('qntx-theme') as ThemeMode | null;
    if (saved === 'light' || saved === 'dark') {
        document.body.dataset.theme = saved;
        return saved;
    }

    // Check system preference
    if (window.matchMedia?.('(prefers-color-scheme: light)').matches) {
        document.body.dataset.theme = 'light';
        return 'light';
    }

    // Default to dark
    document.body.dataset.theme = 'dark';
    return 'dark';
}

// Set up mutation observer for theme changes on body
if (typeof document !== 'undefined') {
    const observer = new MutationObserver((mutations) => {
        for (const mutation of mutations) {
            if (mutation.type === 'attributes' && mutation.attributeName === 'data-theme') {
                const newTheme = getCurrentTheme();
                themeChangeCallbacks.forEach(callback => callback(newTheme));
            }
        }
    });

    // Start observing when document is ready
    if (document.body) {
        observer.observe(document.body, { attributes: true, attributeFilter: ['data-theme'] });
    } else {
        document.addEventListener('DOMContentLoaded', () => {
            observer.observe(document.body, { attributes: true, attributeFilter: ['data-theme'] });
        });
    }
}

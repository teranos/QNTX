import tseslint from 'typescript-eslint';

// No presets. Every rule here is a ban this repo argued for, so a lint failure
// is always a rule somebody wrote down rather than a style opinion.

const BANNED_GLOBALS = [
    {
        name: 'alert',
        message: 'alert() is BANNED. Use Button component error handling (throws from onClick) or log.error()',
    },
    {
        name: 'confirm',
        message: 'confirm() is BANNED. Use Button confirmation property or proper UI confirmation flow',
    },
    {
        name: 'prompt',
        message: 'prompt() is BANNED. Use proper form inputs',
    },
    {
        name: 'toast',
        message: 'toast() is BANNED. Use contextualized error display (Button errors, inline messages, etc.)',
    },
];

const NO_TOAST = {
    selector: "CallExpression[callee.name='toast']",
    message: 'toast() is BANNED. Use contextualized error display in component context',
};

// apiFetch resolves the backend URL, carries credentials, and reports 401 to
// the connectivity manager. A raw fetch does none of that, so an auth failure
// on one is invisible to the UI that is supposed to react to it.
const NO_RAW_FETCH = [
    {
        selector: "CallExpression[callee.name='fetch']",
        message: "fetch() is BANNED. Use apiFetch/apiJson from './client'",
    },
    {
        selector: "CallExpression[callee.object.name='window'][callee.property.name='fetch']",
        message: "window.fetch() is BANNED. Use apiFetch/apiJson from './client'",
    },
    {
        selector: "CallExpression[callee.object.name='globalThis'][callee.property.name='fetch']",
        message: "globalThis.fetch() is BANNED. Use apiFetch/apiJson from './client'",
    },
];

export default [
    {
        // Generated from proto — enriching the generator is the way to change
        // these, so linting them reports on a file nobody edits.
        ignores: ['ts/generated/**'],
    },
    {
        files: ['ts/**/*.ts'],
        languageOptions: { parser: tseslint.parser },
        rules: {
            'no-alert': 'error',
            'no-restricted-globals': ['error', ...BANNED_GLOBALS],
            'no-restricted-syntax': ['error', NO_TOAST, ...NO_RAW_FETCH],
        },
    },
    {
        // client/ is where apiFetch lives and where the connectivity probes sit
        // underneath it. Banning fetch here would ban its implementation.
        files: ['ts/client/**/*.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST],
        },
    },
    {
        // These load a .wasm binary by URL, not a QNTX endpoint. apiFetch would
        // prepend the backend origin, which is the wrong place to look for it.
        files: ['ts/laye.ts', 'ts/ats-wasm.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST],
        },
    },
];

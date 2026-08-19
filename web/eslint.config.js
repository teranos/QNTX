import tseslint from 'typescript-eslint';

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
// the connectivity manager.
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
        // Generated from proto; change the generator.
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
        // client/ is where apiFetch lives.
        files: ['ts/client/**/*.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST],
        },
    },
    {
        // These load a .wasm binary by URL.
        files: ['ts/laye.ts', 'ts/ats-wasm.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST],
        },
    },
    {
        // The liveness probe runs before anything is initialised, and apiFetch
        // reports every answer to the connectivity manager.
        files: ['ts/liveness.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST],
        },
    },
];

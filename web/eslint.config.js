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

// The error axiom, as far as ESLint can carry it: a catch that does not bind
// what it caught can only swallow it.
const SACRED_CATCH = [
    {
        selector: 'CatchClause:not([param])',
        message: 'catch must bind the error it caught: `catch (err)`. A bare `catch {` can only swallow.',
    },
    {
        selector: "CallExpression[callee.property.name='catch'] > ArrowFunctionExpression[params.length=0]",
        message: 'a .catch() handler must take the rejection: `.catch((err) => ...)`. Dropping it in the parameter list is swallowing.',
    },
    {
        selector: "CallExpression[callee.property.name='catch'] > FunctionExpression[params.length=0]",
        message: 'a .catch() handler must take the rejection: `.catch(function (err) ...)`. Dropping it in the parameter list is swallowing.',
    },
];

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
            'no-empty': 'error',
            'no-restricted-globals': ['error', ...BANNED_GLOBALS],
            'no-restricted-syntax': ['error', NO_TOAST, ...NO_RAW_FETCH, ...SACRED_CATCH],
        },
    },
    {
        // Typed linting — tsconfig excludes *.test.ts, so the rule stops there too.
        files: ['ts/**/*.ts'],
        ignores: ['ts/**/*.test.ts'],
        languageOptions: {
            parser: tseslint.parser,
            parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
        },
        plugins: { '@typescript-eslint': tseslint.plugin },
        rules: {
            '@typescript-eslint/no-floating-promises': 'error',
        },
    },
    {
        // client/ is where apiFetch lives.
        files: ['ts/client/**/*.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST, ...SACRED_CATCH],
        },
    },
    {
        // These load a .wasm binary by URL.
        files: ['ts/laye.ts', 'ts/ats-wasm.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST, ...SACRED_CATCH],
        },
    },
    {
        // The liveness probe runs before anything is initialised, and apiFetch
        // reports every answer to the connectivity manager.
        files: ['ts/liveness.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST, ...SACRED_CATCH],
        },
    },
];

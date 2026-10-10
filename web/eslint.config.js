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

// toast() was banned by name, and showToast() walked past it.
const NO_SHOW_TOAST = {
    patterns: [{
        group: ['**/toast', '**/toast.ts'],
        importNames: ['showToast'],
        message: 'showToast() is toast() under another name, and BANNED. Use contextualized error display in component context',
    }],
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
    // Never truncated: a message cut from its start hides the end of what
    // was said. An error being made or thrown, and anything serialised to be
    // said, is said whole.
    {
        selector: ":matches(NewExpression[callee.name='Error'], ThrowStatement) CallExpression[callee.property.name=/^(slice|substring|substr)$/]",
        message: 'cutting an error is BANNED (ERROR AXIOM: never truncated). Say the whole of it.',
    },
    {
        selector: "CallExpression[callee.property.name=/^(slice|substring|substr)$/][callee.object.callee.object.name='JSON'][callee.object.callee.property.name='stringify']",
        message: 'cutting what was serialised to be said is BANNED (ERROR AXIOM: never truncated). Say the whole of it.',
    },
];

// The old-style tooltip — el.title = "…" — is banned in new stand UI: the
// tooltip infra (class "has-tooltip" + data-tooltip, tooltip.attach) carries a
// tooltip the node styles and can make multi-line, and a raw title cannot.
const NO_RAW_TITLE = {
    selector: "AssignmentExpression[left.type='MemberExpression'][left.property.name='title']",
    message: 'raw element.title tooltips are BANNED. Use the tooltip infra: class "has-tooltip" + data-tooltip="…", then tooltip.attach(container).',
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

// "ban 1, 2, 3, 4, 5"

// An absence becoming behaviour, the same kinds nilcheck holds in Go and Rust.
// What already stands is in eslint-suppressions.json, and only falls.
const ABSENCE = [
    { selector: "BinaryExpression[right.type='Literal'][right.value='']", message: 'nil is nil: a test against the empty string gives nothing a meaning, or refuses on it. Name the meaning.' },
    { selector: "BinaryExpression[right.type='Literal'][right.value=0]", message: 'zero means zero: a test against 0 gives nothing a meaning, or refuses on it. Name the meaning.' },
    { selector: "BinaryExpression[right.type='Literal'][right.raw='null']", message: 'nil is nil: a test against null gives nothing a meaning, or refuses on it. Name the meaning.' },
    { selector: "BinaryExpression[right.type='Identifier'][right.name='undefined']", message: 'nil is nil: a test against undefined gives nothing a meaning, or refuses on it. Name the meaning.' },
    { selector: "UnaryExpression[operator='void']", message: 'a discarded value is BANNED: read what came back.' },
    { selector: 'SwitchCase[test=null]', message: 'a catch-all default: is BANNED: name every case, and refuse an unknown one.' },
    { selector: "LogicalExpression[operator='??']", message: 'a fallback is BANNED: a missing value is refused, not replaced with an invented one.' },
    { selector: "LogicalExpression[operator='||'][right.type='Literal']", message: 'a fallback is BANNED: a missing value is refused, not replaced with an invented one.' },
    { selector: "LogicalExpression[operator='||'][right.type='TemplateLiteral']", message: 'a fallback is BANNED: a missing value is refused, not replaced with an invented one.' },
    { selector: "LogicalExpression[operator='||'][right.type='ArrayExpression']", message: 'a fallback is BANNED: a missing value is refused, not replaced with an invented one.' },
    { selector: "LogicalExpression[operator='||'][right.type='ObjectExpression']", message: 'a fallback is BANNED: a missing value is refused, not replaced with an invented one.' },
    { selector: 'CatchClause:not(:has(ThrowStatement)):not(:has(ReturnStatement))', message: 'catching and carrying on is BANNED: throw it on, or return what the failure means.' },
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
            'no-restricted-imports': ['error', NO_SHOW_TOAST],
            'no-restricted-syntax': ['error', NO_TOAST, ...NO_RAW_FETCH, ...SACRED_CATCH, ...ABSENCE],
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
            '@typescript-eslint/no-deprecated': 'warn',
        },
    },
    {
        // client/ is where apiFetch lives.
        files: ['ts/client/**/*.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST, ...SACRED_CATCH, ...ABSENCE],
        },
    },
    {
        // These load a .wasm binary by URL.
        files: ['ts/laye.ts', 'ts/ats-wasm.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST, ...SACRED_CATCH, ...ABSENCE],
        },
    },
    {
        // The liveness probe runs before anything is initialised, and apiFetch
        // reports every answer to the connectivity manager.
        files: ['ts/liveness.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST, ...SACRED_CATCH, ...ABSENCE],
        },
    },
    {
        // The stand UI is where the old-style tooltip ban starts (ADR-035). The
        // repo-wide migration off el.title is its own change; this holds the
        // line for new stand code.
        // TODO: propagate the tooltip infra beyond the stand UI.
        // "i want tooltip to be propagated more"
        files: ['ts/market-element.ts'],
        rules: {
            'no-restricted-syntax': ['error', NO_TOAST, ...NO_RAW_FETCH, ...SACRED_CATCH, NO_RAW_TITLE, ...ABSENCE],
        },
    },
];

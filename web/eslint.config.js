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

// "The elements package should own Tables" "TABLES BEING MAX 4 col"
// A table is createTable from @teranos/elements, whose heads are one to four and whose rows are
// as wide: a fifth column does not compile. A table built by hand stops the build.
const TABLE_IS_ELEMENTS = "a table is @teranos/elements': createTable(name, heads, rows), four columns at most. QNTX does not build a table.";
const NO_RAW_TABLE = [
    { selector: "CallExpression[callee.property.name='createElement'][arguments.0.value=/^(table|thead|tbody|tr|th)$/]", message: TABLE_IS_ELEMENTS },
    { selector: "Literal[value=/<(table|thead|tbody|tr|th)[\\s>]/]", message: TABLE_IS_ELEMENTS },
    { selector: "TemplateElement[value.raw=/<(table|thead|tbody|tr|th)[\\s>]/]", message: TABLE_IS_ELEMENTS },
    // "i wish very deeply that this was made structurally impossible, it should simply not be a thing, ci should fail on it"
    { selector: "CallExpression[callee.name='createTable'][arguments.1.type='ArrayExpression'][arguments.1.elements.length=2]", message: 'a record, a label beside its value, is record() from ts/table-cell.ts: its labels never break inside a word.' },
];

// "i want it to not compile when shit like this happens"
// A control is @teranos/elements': pressFrom, its switch, its fields. One drawn here stops the build.
const CONTROL_IS_ELEMENTS = "a control is @teranos/elements': pressFrom for a button, and its switch and fields. QNTX does not draw a control.";
const NO_RAW_CONTROL = [
    ...['button', 'input', 'select', 'textarea'].map((tag) => ({
        selector: `CallExpression[callee.property.name='createElement'][arguments.0.value='${tag}']`,
        message: CONTROL_IS_ELEMENTS,
    })),
    { selector: "Literal[value=/<(button|input|select|textarea)[\\s>]/]", message: CONTROL_IS_ELEMENTS },
    { selector: "TemplateElement[value.raw=/<(button|input|select|textarea)[\\s>]/]", message: CONTROL_IS_ELEMENTS },
    { selector: "ImportDeclaration[source.value=/components\\/button/]", message: CONTROL_IS_ELEMENTS },
];

// "if it overflows width, hard fail, die"
// What makes content wider than its element, as far as the code says it: a line held unbroken.
const WIDTH = "WIDTH: a line held unbroken is wider than a phone (@teranos/elements drops the element dead). Let it wrap: pre-wrap, pre-line or normal.";
const NO_FIXED_WIDTH = [
    { selector: "AssignmentExpression[left.object.property.name='style'][left.property.name='whiteSpace'][right.value=/^(nowrap|pre)$/]", message: WIDTH },
    { selector: "CallExpression[callee.property.name='setProperty'][arguments.0.value='white-space'][arguments.1.value=/^(nowrap|pre)$/]", message: WIDTH },
];

// "There's no doubt in my mind that Elements owns it's own font sizes."
// "I WANT QNTX WEB TO FAIL"
// Text size is @teranos/elements': sizeText(node, size) with a size from TEXT_SIZES. Written
// anywhere else, it stops the build. A stylesheet is read by font-size-ban.ts.
const TEXT_SIZE_IS_ELEMENTS = "text size is @teranos/elements': sizeText(node, 'xs' | 'sm' | 'md' | 'lg'). QNTX does not size text.";
const NO_FONT_SIZE = [
    { selector: "AssignmentExpression[left.property.name=/^(fontSize|font)$/]", message: TEXT_SIZE_IS_ELEMENTS },
    { selector: "CallExpression[callee.property.name='setProperty'][arguments.0.value=/^font(-size)?$/]", message: TEXT_SIZE_IS_ELEMENTS },
    { selector: "Property[key.name=/^(fontSize|font)$/]", message: TEXT_SIZE_IS_ELEMENTS },
    { selector: "Literal[value=/font-size|(^|[\\s;{\"'])font:/]", message: TEXT_SIZE_IS_ELEMENTS },
    { selector: "TemplateElement[value.raw=/font-size|(^|[\\s;{\"'])font:/]", message: TEXT_SIZE_IS_ELEMENTS },
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
            'no-restricted-syntax': ['error', ...NO_RAW_TABLE, ...NO_RAW_CONTROL, ...NO_FONT_SIZE, NO_TOAST, ...NO_RAW_FETCH, ...SACRED_CATCH, ...ABSENCE, ...NO_FIXED_WIDTH],
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
            'no-restricted-syntax': ['error', ...NO_RAW_TABLE, ...NO_RAW_CONTROL, ...NO_FONT_SIZE, NO_TOAST, ...SACRED_CATCH, ...ABSENCE, ...NO_FIXED_WIDTH],
        },
    },
    {
        // These load a .wasm binary by URL.
        files: ['ts/laye.ts', 'ts/ats-wasm.ts'],
        rules: {
            'no-restricted-syntax': ['error', ...NO_RAW_TABLE, ...NO_RAW_CONTROL, ...NO_FONT_SIZE, NO_TOAST, ...SACRED_CATCH, ...ABSENCE, ...NO_FIXED_WIDTH],
        },
    },
    {
        // The liveness probe runs before anything is initialised, and apiFetch
        // reports every answer to the connectivity manager.
        files: ['ts/liveness.ts'],
        rules: {
            'no-restricted-syntax': ['error', ...NO_RAW_TABLE, ...NO_RAW_CONTROL, ...NO_FONT_SIZE, NO_TOAST, ...SACRED_CATCH, ...ABSENCE, ...NO_FIXED_WIDTH],
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
            'no-restricted-syntax': ['error', ...NO_RAW_TABLE, ...NO_RAW_CONTROL, ...NO_FONT_SIZE, NO_TOAST, ...NO_RAW_FETCH, ...SACRED_CATCH, NO_RAW_TITLE, ...ABSENCE, ...NO_FIXED_WIDTH],
        },
    },
];

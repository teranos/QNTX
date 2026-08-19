// A docstring ends at its closing quote, which may sit on the opening line.
function blockDoc(lines: string[], from: number, quote: string): string {
    const body = [lines[from].trim().slice(3), ...lines.slice(from + 1)];
    const out: string[] = [];
    for (const line of body) {
        const end = line.indexOf(quote);
        if (end !== -1) {
            out.push(line.slice(0, end));
            break;
        }
        out.push(line);
    }
    return out.join('\n').trim();
}

// A leading run of "# ..." is the other way a handler introduces itself.
function hashDoc(lines: string[], from: number): string {
    const out: string[] = [];
    for (let i = from; i < lines.length && lines[i].trim().startsWith('#'); i++) {
        out.push(lines[i].trim().slice(1).trim());
    }
    return out.join('\n').trim();
}

// What a decorator was given, as written: everything inside its parentheses.
function decoratorArgs(code: string, name: string): string | null {
    const marker = `@${name}(`;
    for (const line of code.split('\n')) {
        const trimmed = line.trim();
        if (!trimmed.startsWith(marker)) continue;
        const open = trimmed.indexOf('(');
        const close = trimmed.lastIndexOf(')');
        if (close <= open) return '';
        return trimmed.slice(open + 1, close);
    }
    return null;
}

// The first quoted argument. `@watch('media:specified', context=CONTEXT)` is
// watching media:specified, and the handler is the one that says so.
function firstQuoted(args: string): string {
    for (const quote of ["'", '"']) {
        const open = args.indexOf(quote);
        if (open === -1) continue;
        const close = args.indexOf(quote, open + 1);
        if (close === -1) continue;
        return args.slice(open + 1, close);
    }
    return '';
}

// What a handler declares it is wired to. Null means it declares nothing —
// which is different from a node having no watcher for it.
export function declaredWatch(code: string): string | null {
    const args = decoratorArgs(code, 'watch');
    if (args === null) return null;
    return firstQuoted(args);
}

// A named argument's value, unquoted. `every=86400` is the schedule; the
// description beside it is prose, and taking the first quoted string took that.
function namedArg(args: string, name: string): string {
    const at = args.indexOf(`${name}=`);
    if (at === -1) return '';
    const rest = args.slice(at + name.length + 1);
    const end = rest.indexOf(',');
    const value = (end === -1 ? rest : rest.slice(0, end)).trim();
    if (value.startsWith("'") || value.startsWith('"')) {
        return value.slice(1, -1);
    }
    return value;
}

// How often, not what it is for. Empty means it declares a schedule and says
// nothing about when.
// A handler declared as one. Null when the code never says @handler.
export function declaredHandler(code: string): string | null {
    const args = decoratorArgs(code, 'handler');
    if (args === null) return null;
    return firstQuoted(args) || namedArg(args, 'name');
}

export function declaredSchedule(code: string): string | null {
    const args = decoratorArgs(code, 'schedule');
    if (args === null) return null;
    return namedArg(args, 'every') || firstQuoted(args);
}

// The two ways Python opens a docstring — 34 is the double mark, 39 the single
// — built from their codes so this line carries no mark of its own.
const DOC_MARKS = [String.fromCharCode(34).repeat(3), String.fromCharCode(39).repeat(3)];

// Where the leading docstring stops, because a doused handler keeps its prose
// and comments out everything under it.
function afterLeadingDoc(lines: string[]): number {
    let i = 0;
    while (i < lines.length && lines[i].trim().length === 0) i++;
    if (i >= lines.length) return i;
    const first = lines[i].trim();
    for (const mark of DOC_MARKS) {
        if (!first.startsWith(mark)) continue;
        if (first.slice(3).includes(mark)) return i + 1;
        for (let j = i + 1; j < lines.length; j++) {
            if (lines[j].includes(mark)) return j + 1;
        }
        return lines.length;
    }
    return i;
}

// Doused: nothing under the docstring runs any more. stoke comments the body
// out rather than deleting it, so the stratum stays in the store and stays
// queryable — it just no longer burns.
export function isDoused(code: string): boolean {
    const lines = code.split('\n');
    let commented = 0;
    for (let i = afterLeadingDoc(lines); i < lines.length; i++) {
        const line = lines[i].trim();
        if (line.length === 0) continue;
        if (!line.startsWith('#')) return false;
        commented++;
    }
    // A handler that is only a docstring is empty, not doused.
    return commented > 0;
}

// What a handler says about itself, which the card leads with instead of code.
export function docComment(code: string): string {
    const lines = code.split('\n');
    let i = 0;
    while (i < lines.length && lines[i].trim() === '') i++;
    const first = i < lines.length ? lines[i].trim() : '';
    if (first.startsWith('"""')) return blockDoc(lines, i, '"""');
    if (first.startsWith("'''")) return blockDoc(lines, i, "'''");
    return hashDoc(lines, i);
}

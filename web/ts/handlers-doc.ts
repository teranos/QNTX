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

export function declaredSchedule(code: string): string | null {
    const args = decoratorArgs(code, 'schedule');
    if (args === null) return null;
    return firstQuoted(args) || args.trim();
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

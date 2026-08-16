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

/**
 * The symbols are said in two languages, and this is what keeps them one set.
 *
 * Go declares them in sym/symbols.go; the browser draws them. They are not a
 * wire shape, so proto has no honest home for them — a bag of named string
 * constants is neither a message nor an enum. What replaces the generator is
 * this: read the Go, and fail if the two ever disagree.
 *
 * A symbol changed on one side and not the other is a glyph drawn with the
 * wrong mark, or a command that silently stops matching.
 */

import { describe, test, expect } from 'bun:test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import * as sym from './sym';

const SYMBOLS_GO = resolve(import.meta.dir, '../../sym/symbols.go');

/**
 * Every `Name = "x"` the Go const blocks declare.
 *
 * A const block line is `NAME = "value" // comment`, with the name and value
 * separated by `=`. Anything without a quoted value is a comment or a brace.
 */
function goSymbols(source: string): Map<string, string> {
    const declared = new Map<string, string>();
    let insideConst = false;

    for (const line of source.split('\n')) {
        const trimmed = line.trim();
        if (trimmed.startsWith('const (')) {
            insideConst = true;
            continue;
        }
        if (insideConst && trimmed === ')') {
            insideConst = false;
            continue;
        }
        if (!insideConst || trimmed.startsWith('//')) continue;

        const [before, after] = splitOnce(trimmed, '=');
        if (after === undefined) continue;

        const name = before.trim();
        const opened = after.indexOf('"');
        if (!name || opened === -1) continue;
        const closed = after.indexOf('"', opened + 1);
        if (closed === -1) continue;

        declared.set(name, after.slice(opened + 1, closed));
    }
    return declared;
}

function splitOnce(text: string, on: string): [string, string | undefined] {
    const at = text.indexOf(on);
    if (at === -1) return [text, undefined];
    return [text.slice(0, at), text.slice(at + on.length)];
}

describe('the symbols', () => {
    const declared = goSymbols(readFileSync(SYMBOLS_GO, 'utf8'));
    const drawn = sym as unknown as Record<string, unknown>;

    test('the Go source declares some', () => {
        expect(declared.size).toBeGreaterThan(20);
    });

    test('every one Go declares is the one the browser draws', () => {
        for (const [name, mark] of declared) {
            expect({ [name]: drawn[name] }).toEqual({ [name]: mark });
        }
    });

    test('the browser draws no symbol Go does not declare', () => {
        const extra = Object.keys(drawn)
            .filter(name => typeof drawn[name] === 'string')
            .filter(name => !declared.has(name));
        expect(extra).toEqual([]);
    });
});

describe('the command tables', () => {
    test('every command maps back to the symbol it came from', () => {
        for (const [command, symbol] of Object.entries(sym.CommandToSymbol)) {
            expect(sym.SymbolToCommand[symbol]).toBe(command);
        }
    });

    test('every symbol with a command has a description', () => {
        for (const command of Object.keys(sym.CommandToSymbol)) {
            expect(sym.CommandDescriptions).toHaveProperty(command);
        }
    });
});

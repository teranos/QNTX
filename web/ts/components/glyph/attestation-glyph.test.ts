/**
 * Tests for attestation glyph rendering — attribute value handling.
 */

import { describe, it, expect } from 'bun:test';
import { extractArray, extractObject, renderItem, renderAttributeValue } from './attestation-attrs';
import { isFastaAttribute, isAminoAcidSequence, renderAminoAcidSequence } from './fasta-renderer';

// -- extractArray --

describe('Tim: extractArray', () => {
    it('parses JSON array string', () => {
        const result = extractArray(JSON.stringify([{ a: 1 }, { a: 2 }]));
        expect(result).not.toBeNull();
        expect(result!.length).toBe(2);
    });

    it('parses large array', () => {
        const items = Array.from({ length: 50 }, (_, i) => ({ id: i }));
        expect(extractArray(JSON.stringify(items))!.length).toBe(50);
    });
});

describe('Spike: extractArray rejects', () => {
    it('single-element array', () => expect(extractArray('[1]')).toBeNull());
    it('empty array', () => expect(extractArray('[]')).toBeNull());
    it('object', () => expect(extractArray('{"a": 1}')).toBeNull());
    it('invalid JSON', () => expect(extractArray('nope')).toBeNull());
    it('plain string', () => expect(extractArray('"hello"')).toBeNull());
});

// -- extractObject --

describe('Tim: extractObject', () => {
    it('parses JSON object string', () => {
        const result = extractObject(JSON.stringify({ name: 'Alice', age: 30 }));
        expect(result).not.toBeNull();
        expect(result!['name']).toBe('Alice');
    });
});

describe('Spike: extractObject rejects', () => {
    it('single-key object', () => expect(extractObject('{"a": 1}')).toBeNull());
    it('array', () => expect(extractObject('[1, 2]')).toBeNull());
    it('invalid JSON', () => expect(extractObject('{broken')).toBeNull());
    it('null', () => expect(extractObject('null')).toBeNull());
});

// -- renderItem --

describe('Tim: renderItem objects', () => {
    it('flat key-value', () => {
        const el = renderItem({ color: 'red', size: 'large' });
        expect(el.textContent).toContain('color');
        expect(el.textContent).toContain('red');
        expect(el.textContent).toContain('size');
        expect(el.textContent).toContain('large');
    });

    it('nested objects', () => {
        const el = renderItem({
            name: 'Bob',
            address: { city: 'Amsterdam', country: 'NL' },
        });
        expect(el.textContent).toContain('city');
        expect(el.textContent).toContain('Amsterdam');
    });

    it('nested arrays', () => {
        const el = renderItem({ tags: ['red', 'green', 'blue'] });
        expect(el.textContent).toContain('tags');
        expect(el.textContent).toContain('red');
        expect(el.textContent).toContain('green');
    });

    it('skips null and empty values', () => {
        const el = renderItem({ name: 'X', empty: '', nothing: null });
        expect(el.textContent).toContain('name');
        expect(el.textContent).not.toContain('empty');
        expect(el.textContent).not.toContain('nothing');
    });
});

describe('Tim: renderItem scalars and arrays', () => {
    it('string', () => {
        expect(renderItem('hello').textContent).toBe('hello');
    });

    it('number', () => {
        expect(renderItem(42).textContent).toBe('42');
    });

    it('number array — no brackets', () => {
        const el = renderItem([1, 2, 3]);
        expect(el.textContent).toContain('1');
        expect(el.textContent).toContain('3');
        expect(el.textContent).not.toContain('[');
    });

    it('string array — no quotes', () => {
        const el = renderItem(['apple', 'banana', 'cherry']);
        expect(el.textContent).toContain('apple');
        expect(el.textContent).toContain('banana');
        expect(el.textContent).not.toContain('"');
    });

    it('object array — rendered inline', () => {
        const el = renderItem([
            { name: 'A', value: 1 },
            { name: 'B', value: 2 },
        ]);
        expect(el.textContent).toContain('A');
        expect(el.textContent).toContain('B');
    });
});

describe('Tim: renderItem sub-pager', () => {
    it('large object array (>9) gets pager', () => {
        const items = Array.from({ length: 15 }, (_, i) => ({ id: i }));
        const el = renderItem(items);
        expect(el.querySelectorAll('button').length).toBe(2);
        expect(el.textContent).toContain('1 / 15');
    });

    it('small object array (<=9) renders inline', () => {
        const items = Array.from({ length: 3 }, (_, i) => ({ id: i }));
        const el = renderItem(items);
        expect(el.querySelectorAll('button').length).toBe(0);
    });
});

describe('Tim: renderItem URL pills', () => {
    it('URL values become pills with filename', () => {
        const el = renderItem({
            name: 'test',
            report: 'https://example.com/files/report.pdf',
            data: 'https://example.com/files/export.csv',
        });
        const links = el.querySelectorAll('a');
        expect(links.length).toBe(2);
        expect(links[0].textContent).toContain('report.pdf');
        expect(links[1].textContent).toContain('export.csv');
        expect(el.textContent).toContain('test');
    });

    it('pills are clickable', () => {
        const el = renderItem({ link: 'https://example.com/file.json' });
        const a = el.querySelector('a');
        expect(a).not.toBeNull();
        expect(a!.href).toContain('example.com');
        expect(a!.target).toBe('_blank');
    });

    it('same Schema.org type shares color', () => {
        const el = renderItem({
            a: 'https://x.com/f.pdb',
            b: 'https://x.com/f.cif',
        });
        const pills = el.querySelectorAll('a') as NodeListOf<HTMLElement>;
        expect(pills[0].style.backgroundColor).toBe(pills[1].style.backgroundColor);
    });

    it('different types get different colors', () => {
        const el = renderItem({
            model: 'https://x.com/f.pdb',
            table: 'https://x.com/f.csv',
            image: 'https://x.com/f.png',
        });
        const pills = el.querySelectorAll('a') as NodeListOf<HTMLElement>;
        const colors = new Set(Array.from(pills).map(p => p.style.backgroundColor));
        expect(colors.size).toBe(3);
    });
});

// -- renderAttributeValue --

describe('Tim: renderAttributeValue', () => {
    it('JSON string array (small) — inline', () => {
        const el = renderAttributeValue(JSON.stringify([{ a: 1 }, { b: 2 }]));
        expect(el.textContent).toContain('1');
        expect(el.textContent).toContain('2');
    });

    it('JSON string array (large) — pager', () => {
        const items = Array.from({ length: 12 }, (_, i) => ({ id: i }));
        const el = renderAttributeValue(JSON.stringify(items));
        expect(el.querySelectorAll('button').length).toBe(2);
        expect(el.textContent).toContain('1 / 12');
    });

    it('JSON string object — structured', () => {
        const el = renderAttributeValue(JSON.stringify({ x: 10, y: 20 }));
        expect(el.textContent).toContain('x');
        expect(el.textContent).toContain('10');
    });

    it('plain string — text', () => {
        const el = renderAttributeValue('just text');
        expect(el.textContent).toBe('just text');
    });

    it('native array — inline', () => {
        const el = renderAttributeValue([{ a: 1 }, { b: 2 }]);
        expect(el.textContent).toContain('1');
        expect(el.textContent).toContain('2');
    });

    it('native object — structured', () => {
        const el = renderAttributeValue({ x: 10, y: 20 });
        expect(el.textContent).toContain('x');
        expect(el.textContent).toContain('10');
    });

    it('number — text', () => {
        expect(renderAttributeValue(42).textContent).toBe('42');
    });
});

describe('Jenny: renderAttributeValue nested data', () => {
    it('deep nesting preserved', () => {
        const el = renderAttributeValue(JSON.stringify({
            level1: 'top',
            nested: {
                level2: 'mid',
                deeper: { level3: 'bottom' },
            },
        }));
        expect(el.textContent).toContain('top');
        expect(el.textContent).toContain('mid');
        expect(el.textContent).toContain('bottom');
    });

    it('mixed arrays and objects', () => {
        const el = renderAttributeValue(JSON.stringify({
            items: [{ name: 'first' }, { name: 'second' }],
            tags: ['a', 'b', 'c'],
            count: 5,
        }));
        expect(el.textContent).toContain('first');
        expect(el.textContent).toContain('second');
        expect(el.textContent).toContain('a');
        expect(el.textContent).toContain('5');
    });
});

// -- isFastaAttribute --

describe('Tim: FASTA detection', () => {
    it('identifies FASTA data', () => {
        const attrs = { format: 'fasta', data: '>seq1\nATGC' };
        expect(isFastaAttribute(attrs, 'data')).toBe(true);
        expect(isFastaAttribute(attrs, 'format')).toBe(true);
    });

    it('rejects non-FASTA', () => {
        expect(isFastaAttribute({ format: 'json', data: '{}' }, 'data')).toBe(false);
    });

    it('rejects unrelated keys', () => {
        expect(isFastaAttribute({ format: 'fasta', data: 'x', other: 'y' }, 'other')).toBe(false);
    });
});

// -- Amino acid sequence detection and rendering --

describe('Tim: isAminoAcidSequence', () => {
    it('detects standard amino acid sequence', () => {
        expect(isAminoAcidSequence('MPASAPPRRPRPPPPSLSLLLVLLGLGGRRL')).toBe(true);
    });

    it('rejects short strings', () => {
        expect(isAminoAcidSequence('MPAS')).toBe(false);
    });

    it('rejects nucleotide-only (ATGC)', () => {
        expect(isAminoAcidSequence('AATTATCCGTTCGGACGCAGACACTATGCCA')).toBe(false);
    });

    it('rejects strings with lowercase', () => {
        expect(isAminoAcidSequence('mpasapprrprppppslslll')).toBe(false);
    });

    it('rejects strings with non-AA characters', () => {
        expect(isAminoAcidSequence('MPASAPPRRP123PPSLSLLL')).toBe(false);
    });

    it('rejects normal text', () => {
        expect(isAminoAcidSequence('THIS IS A NORMAL SENTENCE')).toBe(false);
    });
});

describe('Tim: renderAminoAcidSequence', () => {
    it('renders colored spans for each residue', () => {
        const el = renderAminoAcidSequence('MKWV');
        const spans = el.querySelectorAll('span');
        expect(spans.length).toBe(4);
        expect(spans[0].textContent).toBe('M');
        expect(spans[1].textContent).toBe('K');
        expect(spans[2].textContent).toBe('W');
        expect(spans[3].textContent).toBe('V');
    });

    it('hydrophobic residues share color', () => {
        const el = renderAminoAcidSequence('AVIL');
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const color = spans[0].style.color;
        expect(spans[1].style.color).toBe(color);
        expect(spans[2].style.color).toBe(color);
        expect(spans[3].style.color).toBe(color);
    });

    it('charged residues get distinct colors', () => {
        const el = renderAminoAcidSequence('RKDE');
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        // R,K positive — same color
        expect(spans[0].style.color).toBe(spans[1].style.color);
        // D,E negative — same color
        expect(spans[2].style.color).toBe(spans[3].style.color);
        // Positive != negative
        expect(spans[0].style.color).not.toBe(spans[2].style.color);
    });
});

describe('Tim: renderItem detects amino acid sequences', () => {
    it('amino acid string value gets colored rendering', () => {
        const el = renderItem({
            sequence: 'MPASAPPRRPRPPPPSLSLLLVLLGLGGRRL',
        });
        // Should contain colored spans, not plain text
        const spans = el.querySelectorAll('span[style*="color"]');
        expect(spans.length).toBeGreaterThan(20);
    });

    it('normal string value stays plain text', () => {
        const el = renderItem({ name: 'hello world' });
        expect(el.textContent).toContain('hello world');
    });
});

import { describe, it, test, expect } from 'bun:test';
import { docComment, declaredWatch, declaredSchedule, declaredHandler, isDoused } from './handlers-doc';

// The docstring mark, built from its code so this file can hold fixtures.
const M = String.fromCharCode(34).repeat(3);

// A doused handler as stoke leaves it: the prose kept, the body commented out.
const DOUSED = [
    M,
    'Retired. The number is kept so the others still read in order.',
    M,
    '',
    '# doused by stoke on 2026-08-18 by s.b.vanhouten, did:key:z6MknG6SR',
    '',
    `# CONTEXT = 'capy'`,
    '',
    `# @watch('media:specified', context=CONTEXT)`,
    '# def stage(upstream):',
    '#     pass',
].join('\n');

describe('a handler that no longer burns', () => {
    it('is doused when nothing under the docstring runs', () => {
        expect(isDoused(DOUSED)).toBe(true);
    });

    it('is not doused while one live line remains', () => {
        expect(isDoused(DOUSED + '\nimport json')).toBe(false);
    });

    // Nothing running and nothing commented out is an empty file, not a doused one.
    it('is not doused when it is only a docstring', () => {
        expect(isDoused(`${M}Watches the queue.${M}\n`)).toBe(false);
    });

    it('reads a one-line docstring the same way', () => {
        expect(isDoused(`${M}Retired.${M}\n# CONTEXT\n`)).toBe(true);
    });

    it('needs no docstring at all', () => {
        expect(isDoused('# CONTEXT\n# def f(): pass\n')).toBe(true);
    });

    it('leaves a live handler alone', () => {
        expect(isDoused(`@watch('media:specified')\ndef check(x):\n    pass`)).toBe(false);
    });
});

describe('a handler declared as one', () => {
    it('reads the name out of an @handler', () => {
        expect(declaredHandler("@handler('whoistoken')\ndef f(): pass")).toBe('whoistoken');
    });

    it('reads a named argument', () => {
        expect(declaredHandler("@handler(name='frozen')\ndef f(): pass")).toBe('frozen');
    });

    it('says null when the code never declares one', () => {
        expect(declaredHandler('def f(): pass')).toBeNull();
    });
});

describe('what a handler declares about itself', () => {
    it('reads the predicate out of an @watch', () => {
        const code = "@watch('media:specified', context=CONTEXT)\ndef check(upstream):\n    pass";
        expect(declaredWatch(code)).toBe('media:specified');
    });

    it('reads a double-quoted one too', () => {
        expect(declaredWatch('@watch("duck:landed")\ndef f(): pass')).toBe('duck:landed');
    });

    // Declaring nothing and declaring a watch on everything are different
    // answers, and null is the one that means the decorator is absent.
    it('says null when there is no @watch at all', () => {
        expect(declaredWatch('def check():\n    pass')).toBeNull();
    });

    it('says empty when an @watch names nothing', () => {
        expect(declaredWatch('@watch()\ndef f(): pass')).toBe('');
    });

    it('finds a decorator that is not on the first line', () => {
        const code = '# a pond\n\n@watch(\'pond:rippled\')\ndef f(): pass';
        expect(declaredWatch(code)).toBe('pond:rippled');
    });

    it('reads an @schedule the same way', () => {
        expect(declaredSchedule("@schedule('1h')\ndef f(): pass")).toBe('1h');
        expect(declaredSchedule('def f(): pass')).toBeNull();
    });

    // every= is how often. The description beside it is prose, and taking the
    // first quoted string put the prose where the interval belongs.
    it('takes the interval and not the description', () => {
        const code = "@schedule(every=86400, description='Can a handler import a sibling')\ndef f(): pass";
        expect(declaredSchedule(code)).toBe('86400');
    });

    it('takes a quoted interval when every= is quoted', () => {
        expect(declaredSchedule("@schedule(every='12h', description='x')\ndef f(): pass")).toBe('12h');
    });
});

test('a docstring on its own lines is the doc', () => {
    const code = '"""\nWatches the queue.\n\nRuns every tick.\n"""\nimport os\n';
    expect(docComment(code)).toBe('Watches the queue.\n\nRuns every tick.');
});

test('a one-line docstring closes on its opening line', () => {
    expect(docComment('"""Watches the queue."""\nimport os\n')).toBe('Watches the queue.');
});

test('single quotes say the same thing', () => {
    expect(docComment("'''Watches the queue.'''\n")).toBe('Watches the queue.');
});

test('a leading hash run is a doc too', () => {
    expect(docComment('# Watches the queue.\n# Every tick.\nimport os\n')).toBe('Watches the queue.\nEvery tick.');
});

test('a hash run after the imports is not the doc', () => {
    expect(docComment('import os\n# not a doc\n')).toBe('');
});

test('code that says nothing about itself says nothing', () => {
    expect(docComment('import os\nprint(1)\n')).toBe('');
});

test('leading blank lines do not hide the doc', () => {
    expect(docComment('\n\n"""Watches."""\n')).toBe('Watches.');
});

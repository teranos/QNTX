import { describe, it, test, expect } from 'bun:test';
import { docComment, declaredWatch, declaredSchedule } from './handlers-doc';

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

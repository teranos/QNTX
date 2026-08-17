import { test, expect } from 'bun:test';
import { docComment } from './handlers-doc';

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

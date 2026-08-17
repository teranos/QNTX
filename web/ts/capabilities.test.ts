import { test, expect } from 'bun:test';
import { sigmaBelongsHere, namespacesBelongHere } from './capabilities';

test('sqlite distills, so sigma belongs there', () => {
    expect(sigmaBelongsHere('sqlite')).toBe(true);
});

test('parquet is not a distillation target, so sigma does not belong', () => {
    expect(sigmaBelongsHere('parquet')).toBe(false);
});

test('parquet has namespaces', () => {
    expect(namespacesBelongHere('parquet')).toBe(true);
});

test('sqlite keeps one universe, so it has no namespaces to show', () => {
    expect(namespacesBelongHere('sqlite')).toBe(false);
});

// Before the node has said, nothing has been ruled out — an empty store must
// not read as parquet, or a sqlite node loses sigma until the message lands.
test('an unanswered store rules nothing out', () => {
    expect(sigmaBelongsHere('')).toBe(true);
    expect(namespacesBelongHere('')).toBe(false);
});

import { expect, test } from 'bun:test';
import { sessionsOf, turnsOf, type Attestation } from './transcript';

function streamed(id: string, predicate: string, session: string, at: string, attributes: Record<string, unknown>): Attestation {
    return { id, subjects: ['user/QNTX:datapunt-owns-the-reference'], predicates: [predicate], contexts: ['session:' + session], timestamp: at, source: 'ground', attributes };
}

const aSession = [
    streamed('7', 'Stop', 's-1', '2026-10-01T22:00:06Z', { last_assistant_message: 'QNTX builds here.' }),
    streamed('1', 'UserPromptSubmit', 's-1', '2026-10-01T22:00:00Z', { prompt: 'Build QNTX here' }),
    streamed('2', 'PreToolUse', 's-1', '2026-10-01T22:00:01Z', { tool_name: 'Bash', file_path: null, command: 'make cli' }),
    streamed('3', 'GroundedPreToolUse', 's-1', '2026-10-01T22:00:02Z', { control: 'no-comment-blocks' }),
    streamed('4', 'PreToolUse', 's-1', '2026-10-01T22:00:03Z', { tool_name: 'Read', file_path: '/home/user/QNTX/Makefile', command: null }),
    streamed('5', 'PreToolUse', 's-1', '2026-10-01T22:00:04Z', { tool_name: 'Grep', file_path: null, command: null }),
    streamed('6', 'ritual:rite', 's-1', '2026-10-01T22:00:05Z', { rite: 'built', verdict: 'advance', code: 0 }),
];

// Tim: a session reads back as what was said and done, in order, each turn
// naming the attestation it came from.
test('a transcript is the session in order', () => {
    const turns = turnsOf(aSession);
    expect(turns.map(t => [t.speaker, t.text])).toEqual([
        ['human', 'Build QNTX here'],
        ['tool', 'make cli'],
        ['ground', 'no-comment-blocks on PreToolUse'],
        ['read', '/home/user/QNTX/Makefile'],
        ['search', 'Grep'],
        ['rite', 'built advance 0'],
        ['assistant', 'QNTX builds here.'],
    ]);
    expect(turns[0].of).toBe('1');
});

// Spike: the same attestation twice is one turn, a sigma is none, and an event a
// transcript does not read is not a turn.
test('one attestation is one turn, and a sigma holds none', () => {
    const sigma = streamed('AS-distill-1', 'PreToolUse', 's-1', '2026-10-01T23:00:00Z', { _total: 46 });
    sigma.source = 'distill';
    const other = streamed('9', 'immediate:ci-status', 's-1', '2026-10-01T22:00:00Z', {});
    expect(turnsOf([aSession[1], aSession[1], sigma, other])).toHaveLength(1);
});

// Jenny: sessions come newest first, each opened by its first prompt.
test('sessions are newest first, opened by their first prompt', () => {
    const sessions = sessionsOf([
        streamed('a', 'UserPromptSubmit', 'older', '2026-10-01T20:00:00Z', { prompt: 'first' }),
        streamed('b', 'UserPromptSubmit', 'newer', '2026-10-01T21:00:00Z', { prompt: 'opening' }),
        streamed('c', 'UserPromptSubmit', 'newer', '2026-10-01T21:30:00Z', { prompt: 'later' }),
        streamed('d', 'GroundedUserPromptSubmit', 'grounded', '2026-10-01T22:00:00Z', { control: 'x' }),
    ]);
    expect(sessions.map(s => [s.session, s.opening])).toEqual([['newer', 'opening'], ['older', 'first']]);
});

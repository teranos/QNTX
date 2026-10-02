import { expect, test } from 'bun:test';
import { msOf, transcriptsIn } from './transcript';

// Tim: a transcripts read answer is its transcripts, turns as the node gave them.
test('a transcripts read answer is its transcripts', () => {
    const answer = {
        transcripts: [{
            session: 's-1', subjects: [], started: '2026-10-01T22:00:00Z', ended: '2026-10-01T22:00:06Z', folded: 0,
            turns: [{ at: '2026-10-01T22:00:00Z', speaker: 'human', text: 'Build QNTX here', of: 'ground:payload:UserPromptSubmit:1' }],
        }],
    };
    const [t] = transcriptsIn(answer);
    expect(t.session).toBe('s-1');
    expect(t.turns[0].of).toBe('ground:payload:UserPromptSubmit:1');
    expect(msOf(t.turns[0].at)).toBe(Date.UTC(2026, 9, 1, 22, 0, 0));
});

// Spike: an answer without transcripts is none, not a throw.
test('an answer without transcripts is none', () => {
    expect(transcriptsIn(null)).toEqual([]);
    expect(transcriptsIn({})).toEqual([]);
    expect(transcriptsIn({ transcripts: 'nope' })).toEqual([]);
});

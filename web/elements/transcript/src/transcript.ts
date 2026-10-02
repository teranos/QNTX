// A transcript is one session Ground recorded, read in the order it happened.
// "I want to kill it, and make sure QNTX takes over"
// The node derives it (transcripts read); the element draws what it answers.

// One thing said or done in a session, naming the attestation it was read from (protocol.Turn).
export interface Turn {
    at: string;
    speaker: string;
    text: string;
    of: string;
}

// One session as transcripts read answers it (protocol.Transcript).
export interface Transcript {
    session: string;
    subjects: string[];
    started: string;
    ended: string;
    turns: Turn[];
    folded: number;
}

// The transcripts in a transcripts read answer, or none when it holds none.
export function transcriptsIn(answer: unknown): Transcript[] {
    const held = (answer as { transcripts?: Transcript[] } | null)?.transcripts;
    return Array.isArray(held) ? held : [];
}

// A turn's time as milliseconds, which the spacers and the warp measure in.
export function msOf(at: string): number {
    return Date.parse(at);
}

// A transcript is one session Ground recorded, read in the order it happened.
// "I want to kill it, and make sure QNTX takes over"
// Loom's reading, done by an element over what the namespace already holds.

// The fields of an attestation a transcript reads, as ui.attestations() answers them.
export interface Attestation {
    id: string;
    subjects: string[];
    predicates: string[];
    contexts: string[];
    timestamp: string;
    source?: string;
    attributes?: Record<string, unknown>;
}

// One thing said or done in a session, naming the attestation it was read from.
export interface Turn {
    at: number;
    speaker: string;
    text: string;
    of: string;
}

export interface Session {
    session: string;
    started: number;
    opening: string;
}

// The session a context names, or empty.
export function sessionOf(contexts: string[]): string {
    for (const context of contexts) {
        if (context.startsWith('session:')) return context.slice('session:'.length);
    }
    return '';
}

function attr(as: Attestation, key: string): string {
    const value = as.attributes?.[key];
    return typeof value === 'string' ? value : '';
}

// A tool call as loom labelled it. A search names its tool: its pattern stays on
// the machine that ran it.
function toolTurn(tool: string, path: string, command: string): [string, string] {
    switch (tool) {
        case 'Bash': return ['tool', command];
        case 'Edit': case 'MultiEdit': case 'NotebookEdit': return ['edit', path];
        case 'Write': return ['write', path];
        case 'Read': return ['read', path];
        case 'Grep': case 'Glob': return ['search', tool];
    }
    return ['tool', tool];
}

// What a rite found: its name, its verdict and the code it read.
function riteSaid(as: Attestation): string {
    let said = attr(as, 'rite') + ' ' + attr(as, 'verdict');
    const code = as.attributes?.['code'];
    if (typeof code === 'number') said += ' ' + Math.trunc(code);
    return said;
}

// One event as a turn, with loom's speakers, or null when a transcript does not
// read it. A sigma (ADR-020) holds no turn.
export function turnOf(as: Attestation): Turn | null {
    if (as.source === 'distill' || as.predicates.length === 0) return null;
    const predicate = as.predicates[0];
    let speaker: string;
    let text: string;
    switch (predicate) {
        case 'UserPromptSubmit': [speaker, text] = ['human', attr(as, 'prompt')]; break;
        case 'Stop': [speaker, text] = ['assistant', attr(as, 'last_assistant_message')]; break;
        case 'PreToolUse': [speaker, text] = toolTurn(attr(as, 'tool_name'), attr(as, 'file_path'), attr(as, 'command')); break;
        case 'SessionStart': case 'SessionEnd':
            [speaker, text] = ['session', predicate.slice('Session'.length) + ' ' + attr(as, 'source')]; break;
        case 'PreCompact': [speaker, text] = ['compaction', 'compacted']; break;
        case 'SubagentStart': case 'SubagentStop':
            [speaker, text] = ['agent', predicate.slice('Subagent'.length) + ' ' + attr(as, 'agent_type')]; break;
        case 'TaskCompleted': [speaker, text] = ['task', attr(as, 'task_subject')]; break;
        case 'ritual:rite': [speaker, text] = ['rite', riteSaid(as)]; break;
        default:
            if (!predicate.startsWith('Grounded')) return null;
            [speaker, text] = ['ground', attr(as, 'control') + ' on ' + predicate.slice('Grounded'.length)];
    }
    return { at: Date.parse(as.timestamp), speaker, text: text.trim(), of: as.id };
}

// Every turn of one session, in the order it happened. One attestation is one turn.
export function turnsOf(events: Attestation[]): Turn[] {
    const read = new Set<string>();
    const turns: Turn[] = [];
    for (const as of events) {
        if (read.has(as.id)) continue;
        read.add(as.id);
        const turn = turnOf(as);
        if (turn) turns.push(turn);
    }
    turns.sort((a, b) => a.at - b.at || (a.of < b.of ? -1 : a.of > b.of ? 1 : 0));
    return turns;
}

// The sessions prompts were given in, newest first, each opened by its first prompt.
export function sessionsOf(prompts: Attestation[]): Session[] {
    const by = new Map<string, Session>();
    for (const as of prompts) {
        if (as.predicates[0] !== 'UserPromptSubmit' || as.source === 'distill') continue;
        const session = sessionOf(as.contexts);
        if (!session) continue;
        const at = Date.parse(as.timestamp);
        const held = by.get(session);
        if (!held || at < held.started) by.set(session, { session, started: at, opening: attr(as, 'prompt') });
    }
    return [...by.values()].sort((a, b) => b.started - a.started);
}

#!/usr/bin/env bun
// The live gate's verdict is of a commit: the node it asks runs that commit, several looks in a row, before it is asked.
// A push once restarted the node mid-run, and every element was named for the 502s of a box taking up the commit.

function needed(name: string): string {
    const value = process.env[name];
    if (value === undefined || value === '') {
        console.error(`node-runs: ${name} is not set`);
        process.exit(1);
    }
    return value;
}

const api = needed('QNTX_GATE_API');
const token = needed('QNTX_GATE_TOKEN');
const sha = needed('QNTX_GATE_SHA');

const LOOK_EVERY_MS = 10_000;
const LOOKS_IN_A_ROW = 6;
const WAIT_MS = 30 * 60_000;

/** What the node says it runs, or why it said nothing. */
async function runs(): Promise<string> {
    try {
        const answer = await fetch(`${api}/am/version`, { headers: { Authorization: `Bearer ${token}` } });
        if (!answer.ok) return `HTTP ${answer.status}`;
        const body = await answer.json() as { commit_hash?: string };
        if (body.commit_hash === undefined) return 'an answer with no commit_hash';
        return body.commit_hash;
    } catch (err) {
        return `no answer: ${String(err)}`;
    }
}

const until = Date.now() + WAIT_MS;
let inARow = 0;
let last = '';
while (Date.now() < until) {
    last = await runs();
    inARow = last === sha ? inARow + 1 : 0;
    console.log(`${new Date().toISOString()} ${api} runs ${last}${last === sha ? ` (${inARow} of ${LOOKS_IN_A_ROW})` : `, not ${sha}`}`);
    if (inARow === LOOKS_IN_A_ROW) process.exit(0);
    await Bun.sleep(LOOK_EVERY_MS);
}
console.error(`node-runs: ${api} did not run ${sha} for ${LOOKS_IN_A_ROW} looks in ${WAIT_MS / 60_000} minutes; it said ${last}`);
process.exit(1);

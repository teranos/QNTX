/**
 * The Sacred Error's one render (teranos/sacred-error), mounted in the flow of a log.
 */

// "YES, TAKE SACRED-ERROR'S OWN RENDER"

// The render draws itself at the cursor a failure carries, and leaves placing
// to its host where no cursor fired it. A log is such a host: each failure sits
// in the flow, in the order the log gives it.

import { define, type SacredErrorElement } from 'sacred-error/element';
import type { Context, SacredError } from 'sacred-error';

export type { SacredError };

/**
 * One failure through the one render. Dismissing hides it and destroys
 * nothing, and the log it stays in is where it is brought back from.
 */
export function sacredEntry(error: SacredError): HTMLElement {
    define();
    const entry = document.createElement('div');
    entry.className = 'sacred-entry';

    const block = document.createElement('sacred-error') as SacredErrorElement;
    block.error = error;
    // A hidden block is pressed only by the press that dismissed it: that press is the render's own.
    block.addEventListener('click', (e) => { if (block.hidden) e.stopPropagation(); });

    const back = document.createElement('button');
    back.className = 'sacred-back';
    back.textContent = `dismissed: ${error.title}. Bring it back`;
    back.hidden = true;
    block.addEventListener('dismissed', () => { back.hidden = false; });
    back.addEventListener('click', (e) => {
        e.stopPropagation();
        block.hidden = false;
        back.hidden = true;
    });

    entry.append(block, back);
    return entry;
}

/**
 * An API error as its turn says it (server/transcripts.go turnOf): which
 * error, a colon, and what Claude Code wrote of it, whole.
 */
export function apiError(id: string, at: string, said: string, context: Context): SacredError {
    const colon = said.indexOf(': ');
    return {
        id,
        severity: 'error',
        context,
        title: colon < 0 ? said : said.substring(0, colon),
        why: colon < 0 ? '' : said.substring(colon + 2),
        at,
    };
}

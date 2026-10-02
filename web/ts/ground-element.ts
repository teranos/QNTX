/**
 * Ground Element — aware of anything Ground.
 */

// "The ground element would be aware of anything ground"
// "Transcripts in Transcript / List of sessions in ground"
// "In the Tray, like Users, DB, Plugins"

// Ground lists the sessions it recorded in this namespace; pressing one opens
// it as its own Transcript, the way Users opens a User.

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { log, SEG } from './logger';
import { Ground } from './sym';
import { openTranscriptElement, when, type TranscriptRead } from './components/element/transcript-element';

const ELEMENT_ID = 'ground-element';

/** Exported for tests: the sessions Ground recorded here, each a button named by its first prompt. */
export function renderSessions(body: HTMLElement, reads: TranscriptRead[], onChoose: (session: string) => void): void {
    body.replaceChildren();
    if (reads.length === 0) {
        const none = document.createElement('div');
        none.className = 'tr-said';
        none.textContent = 'No session Ground recorded is in this namespace.';
        body.appendChild(none);
        return;
    }
    for (const read of reads) {
        const row = document.createElement('button');
        row.className = 'tr-session';
        const at = document.createElement('span');
        at.className = 'tr-when';
        at.textContent = when(read.started);
        const opening = document.createElement('span');
        opening.className = 'tr-opening';
        opening.textContent = read.turns.find(t => t.speaker === 'human')?.text ?? read.session;
        row.append(at, opening);
        row.addEventListener('click', () => { onChoose(read.session); });
        body.appendChild(row);
    }
}

export function createGroundElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Ground',
        symbol: Ground,
        renderContent: () => {
            const body = document.createElement('div');
            body.className = 'ground';
            const reading = document.createElement('div');
            reading.className = 'tr-said';
            reading.textContent = 'Reading the sessions Ground recorded here…';
            body.appendChild(reading);
            apiJson<{ transcripts: TranscriptRead[] }>('/api/transcripts')
                .then(answer => renderSessions(body, answer.transcripts, openTranscriptElement))
                .catch((err: unknown) => {
                    log.error(SEG.UI, '[GroundElement] the node did not list the sessions Ground recorded', err);
                    reading.textContent = `the node did not list the sessions Ground recorded: ${err instanceof Error ? err.message : String(err)}`;
                    body.replaceChildren(reading);
                });
            return body;
        },
    };
}

/** Opens the Ground element. */
export function openGroundElement(): void {
    tray.open(ELEMENT_ID);
}

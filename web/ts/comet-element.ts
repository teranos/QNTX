/**
 * Comet element — one repository as a comet: the ground built from it, landing on earth.
 */

// "i can click on a comet"
// "it opens the comet element"
// "a comet can be in the observed state, the procedure is to click the observe button"
// "comet changes shape"

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { Comet as CometSym } from './sym';
import { cometAlone, type Comet } from './ground-scene';

// What each state says, in the element's own words.
const STATE_SAID: Record<Comet['state'], string> = {
    observed: 'Observed. When main moves, ground moves.',
    observing: 'Observing. Not observed yet: the first time main moves, the first build lands.',
    unobserved: 'Not observed.',
};

const NOT_YET = 'Observing a comet, and the build it starts, are not in the node yet: nothing here starts or stops one.';

/** Exported for tests: the element's body, the comet alone and what state it is in. */
export function cometBody(c: Comet): HTMLElement {
    const body = document.createElement('div');
    body.className = 'comet';

    const plate = document.createElement('div');
    plate.className = 'comet-plate';
    plate.appendChild(cometAlone(c));
    const name = document.createElement('header');
    name.className = 'gr-name';
    const of = document.createElement('b');
    of.textContent = 'Comet';
    const repo = document.createElement('span');
    repo.className = 'comet-repo';
    repo.textContent = c.repo;
    name.append(of, repo);
    plate.appendChild(name);

    const said = document.createElement('div');
    said.className = `comet-state comet-state-${c.state}`;
    said.textContent = STATE_SAID[c.state];

    const limit = document.createElement('p');
    limit.className = 'gr-limit';
    const tape = document.createElement('span');
    tape.textContent = NOT_YET;
    limit.appendChild(tape);

    body.append(plate, said, limit);
    return body;
}

/** Opens one repository's comet. Called from Ground, by a press on its head. */
export function openCometElement(c: Comet): void {
    const itemId = `comet-${c.repo}`;
    if (tray.has(itemId)) {
        tray.open(itemId);
        return;
    }
    tray.add({
        id: itemId,
        title: `Comet ${c.repo}`,
        symbol: CometSym,
        // The night the comet crosses: the same navy its band in Ground wears.
        color: 'var(--night-navy)',
        onClose: () => { tray.remove(itemId); },
        renderContent: () => cometBody(c),
    } satisfies Element);
    tray.open(itemId);
}

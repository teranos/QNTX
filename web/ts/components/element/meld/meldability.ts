/**
 * Which kinds of element meld, and on which side: QNTX's to say (@teranos/elements 1.18.0,
 * configureElements({ meldability })). Unsaid, nothing melds.
 *
 * The table @teranos/elements carried itself until 1.18.0, as it stood.
 *
 * - right: horizontal data flow (ax → py → prompt, py → py)
 * - bottom: result/output attachment (py ↓ result, prompt ↓ result)
 * - top: a subcanvas only
 */

import type { Meldability } from '@teranos/elements';

/** Every element class that melds. */
const ALL_ELEMENT_CLASSES = [
    'canvas-ax-element', 'canvas-se-element', 'canvas-py-element',
    'canvas-prompt-element', 'canvas-doc-element', 'canvas-note-element',
    'canvas-result-element', 'canvas-subcanvas-element',
    'canvas-plugin-element',
] as const;

export const MELDABILITY: Meldability = {
    'canvas-ax-element': [
        { direction: 'right', targets: ['canvas-prompt-element', 'canvas-py-element', 'canvas-subcanvas-element'] },
    ],
    'canvas-se-element': [
        { direction: 'right', targets: ['canvas-prompt-element', 'canvas-py-element', 'canvas-se-element', 'canvas-subcanvas-element'] },
    ],
    'canvas-py-element': [
        { direction: 'right', targets: ['canvas-prompt-element', 'canvas-py-element', 'canvas-subcanvas-element'] },
        { direction: 'bottom', targets: ['canvas-result-element', 'canvas-subcanvas-element'] },
    ],
    'canvas-prompt-element': [
        { direction: 'bottom', targets: ['canvas-result-element', 'canvas-subcanvas-element'] },
    ],
    'canvas-doc-element': [
        { direction: 'right', targets: ['canvas-result-element', 'canvas-prompt-element', 'canvas-doc-element', 'canvas-subcanvas-element'] },
        { direction: 'bottom', targets: ['canvas-prompt-element', 'canvas-doc-element', 'canvas-subcanvas-element'] },
    ],
    'canvas-note-element': [
        { direction: 'bottom', targets: ['canvas-prompt-element', 'canvas-plugin-element', 'canvas-subcanvas-element'] },
    ],
    'canvas-result-element': [
        { direction: 'bottom', targets: ['canvas-result-element'] },
    ],
    'canvas-plugin-element': [
        { direction: 'bottom', targets: ['canvas-result-element', 'canvas-subcanvas-element'] },
    ],
    'canvas-subcanvas-element': [
        { direction: 'right', targets: ALL_ELEMENT_CLASSES },
        { direction: 'bottom', targets: ALL_ELEMENT_CLASSES },
        { direction: 'top', targets: ALL_ELEMENT_CLASSES },
    ],
};

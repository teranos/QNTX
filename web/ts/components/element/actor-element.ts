/**
 * Actor Element (⌬) — who stood behind a claim, as a thing in its own right.
 *
 * An actor is not a segment of the triple: the triple is the claim, and the
 * actor is the one part of an attestation that can be trusted, doubted or
 * disagreed with. It is built on the same machinery because what it shows takes
 * the same shape, not because it is the same kind of thing.
 *
 * What it shows: the predicates this actor is often paired with, and the
 * contexts. Both are pressable and open theirs.
 */

import { BY } from '../../sym';
import { log, SEG } from '../../logger';
import { openSegmentElement, renderSegmentStats, segmentElementId, type Segment } from './segment-element';
import { pressable } from './segment-press';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

/** Built when asked rather than held at module scope (see predicate-element.ts). */
export function actorSegment(): Segment {
    return {
        kind: 'actor',
        symbol: BY,
        param: 'actor',
        filed: () => 'attested by this actor',
        reading: (value) => `Reading what ${value} attested…`,
        sections: [
            {
                label: 'Often paired with',
                className: 'actor-predicates',
                pick: (a: Attestation) => a.predicates,
                cell: pressable('predicate'),
            },
            {
                label: 'In these contexts',
                className: 'actor-contexts',
                pick: (a: Attestation) => a.contexts,
                cell: pressable('context'),
            },
        ],
    };
}

/** One element per actor, so its id says which actor it is about. */
export function actorElementId(actor: string): string {
    return segmentElementId(actorSegment(), actor);
}

/** Exported for tests: the sections, in the order they are shown. */
export function renderActorStats(container: HTMLElement, actor: string, attestations: Attestation[]): void {
    renderSegmentStats(container, actorSegment(), actor, attestations);
}

/** Opens one actor as its own element. */
export function openActorElement(actor: string): void {
    log.debug(SEG.ELEMENT, `[ActorElement] opening ${actor}`);
    openSegmentElement(actorSegment(), actor);
}

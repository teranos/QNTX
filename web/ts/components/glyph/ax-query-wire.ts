/**
 * Pure mappings between a parsed AX query and the two stores that answer it:
 * the local IndexedDB filter (resolved instants) and the REST query string
 * (the raw expressions, which /api/attestations speaks itself).
 *
 * No WASM import — everything here is data-shaping, testable without the
 * module loaded.
 */

import type { AxLocalFilter, AxQuery, ResolvedAxQuery, ResolvedTemporal } from '../../ats-wasm';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

/** Epoch-ms bounds a resolved temporal clause imposes. Over is a duration,
 * not a range, so it imposes none. */
export function temporalBounds(temporal?: ResolvedTemporal): { time_start?: number; time_end?: number } {
    if (!temporal) return {};
    if ('Since' in temporal) return { time_start: temporal.Since };
    if ('Until' in temporal) return { time_end: temporal.Until };
    if ('On' in temporal) return { time_start: temporal.On.start_ms, time_end: temporal.On.end_ms };
    if ('Between' in temporal) return { time_start: temporal.Between.start_ms, time_end: temporal.Between.end_ms };
    return {};
}

/** Build the local store filter from a resolved query. */
export function resolvedToFilter(query: ResolvedAxQuery): AxLocalFilter {
    return {
        subjects: query.subjects,
        predicates: query.predicates,
        contexts: query.contexts,
        actors: query.actors,
        ...temporalBounds(query.temporal),
    };
}

/**
 * Build the /api/attestations query string from a raw parse. The temporal
 * clause rides as the words the user typed — the server accepts the same
 * expressions the ax language does, so nothing is resolved here.
 */
export function axQueryParams(query: AxQuery): string {
    const params = new URLSearchParams();
    if (query.subjects.length > 0) params.set('subject', query.subjects.join(','));
    if (query.predicates.length > 0) params.set('predicate', query.predicates.join(','));
    if (query.contexts.length > 0) params.set('context', query.contexts.join(','));
    if (query.actors.length > 0) params.set('actor', query.actors.join(','));

    const t = query.temporal;
    if (t) {
        if ('Since' in t) params.set('since', t.Since);
        if ('Until' in t) params.set('until', t.Until);
        if ('On' in t) params.set('on', t.On);
        if ('Between' in t) {
            params.set('since', t.Between[0]);
            params.set('until', t.Between[1]);
        }
        // Over is a duration comparison, not a range — the REST surface has
        // no word for it, so it stays out of the query string.
    }

    return params.toString();
}

/** An attestation as GET /api/attestations returns it (Go types.As JSON):
 * timestamps are RFC3339 strings, not the proto's epoch milliseconds. */
export interface WireAs {
    id: string;
    subjects: string[];
    predicates: string[];
    contexts: string[];
    actors: string[];
    timestamp: string;
    source: string;
    attributes?: { [key: string]: unknown };
    created_at: string;
    signer_did?: string;
}

/** Convert a REST attestation to the proto shape the glyph renders. */
export function fromWireAs(a: WireAs): Attestation {
    return {
        id: a.id,
        subjects: a.subjects ?? [],
        predicates: a.predicates ?? [],
        contexts: a.contexts ?? [],
        actors: a.actors ?? [],
        timestamp: a.timestamp ? Date.parse(a.timestamp) : 0,
        source: a.source ?? '',
        attributes: a.attributes as { [key: string]: any } | undefined,
        created_at: a.created_at ? Date.parse(a.created_at) : 0,
        signature: new Uint8Array(0),
        signer_did: a.signer_did ?? '',
    };
}

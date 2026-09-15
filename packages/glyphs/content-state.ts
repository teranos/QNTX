/**
 * What a glyph's body is showing.
 *
 * A manifestation with chrome and an empty body is three different things —
 * still loading, nothing to show, or a failure nobody heard — and until this
 * list existed the element said none of them. An empty box was read as whichever
 * of the three the reader guessed.
 *
 * So emptiness is declared rather than inferred, the way `manifestation.ts`
 * declares which manifestation a glyph is in. The states are a closed set for
 * the same reason that one is: a name off the list is a word, not a state.
 *
 * `refused` is not `error`. docs/sentry.md: "A refusal that the node is designed
 * to answer with is neither [Error nor Warn] — it is Info, and it is the node
 * working." A body that never arrived is the node not working, and it is shown
 * where it happened, because logging alone is hiding (web/ts/market-glyph.ts).
 */

/**
 * Every state a glyph's body can be in, and whether it is done moving.
 *
 * `settled` is what the watcher dispatches on: a body still `pending` past its
 * deadline is the failure this list exists to name. Written as a literal
 * `true`/`false` rather than `boolean` so a caller can narrow on it.
 */
export interface ContentStateTable {
    /**
     * Mounted and showing nothing. Written by the runtime at mount
     * (manifestations/render-content.ts), never by a glyph — a glyph that means
     * to show nothing says `empty` and says it in words.
     */
    readonly pending: { readonly settled: false };

    /**
     * Showing something. Written by the runtime the moment the body draws:
     * at mount when the glyph rendered synchronously, or when what it was
     * waiting for arrives.
     */
    readonly present: { readonly settled: true };

    /**
     * Showing that there is nothing — "No access tokens." The glyph's own
     * words, so the glyph declares it (`declareContent`). The runtime cannot
     * tell this from `present`, and must not: both draw.
     */
    readonly empty: { readonly settled: true };

    /**
     * Nothing arrived and nothing said why. Written by the runtime when the
     * deadline passes with the body still `pending`, and by the content
     * boundary when `renderContent()` throws.
     */
    readonly refused: { readonly settled: true };
}

const TABLE: ContentStateTable = {
    pending: { settled: false },
    present: { settled: true },
    empty: { settled: true },
    refused: { settled: true },
};

export const CONTENT_STATES: ContentStateTable = Object.freeze(TABLE);

// Each row too, not just the table — isSettled reads the table live.
for (const row of Object.values(CONTENT_STATES)) Object.freeze(row);

export type ContentState = keyof ContentStateTable;

/** Whether a name is in the table. Own keys only — `toString` is not a state. */
export function isContentState(name: string): name is ContentState {
    return Object.prototype.hasOwnProperty.call(CONTENT_STATES, name);
}

/** Whether a state is done moving. An unknown name is not settled. */
export function isSettled(name: string): boolean {
    return isContentState(name) && CONTENT_STATES[name].settled;
}

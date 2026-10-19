/**
 * Local Semantic Search
 *
 * Offline cosine similarity search over locally-cached embeddings.
 * Uses WASM cosine_similarity_f32 for computation — no server round-trip.
 *
 * Query vectors must have been previously fetched from the server
 * (local embedding generation is out of scope for v2).
 */

import { embeddingStore } from './embedding-store';
import { cosineSimilarity } from './ats-wasm';
import { log, SEG } from './logger';
import { timed } from './wasm-metrics';

export interface LocalSearchResult {
    attestationId: string;
    similarity: number;
}

/**
 * Search locally-cached embeddings by cosine similarity against a query vector.
 *
 * @param queryVector - Pre-computed embedding vector (e.g. from server's /api/embeddings/generate)
 * @param limit - Maximum number of results to return
 * @param threshold - Minimum similarity score (0.0 - 1.0)
 * @returns Results sorted by similarity descending
 */
export async function localSemanticSearch(
    queryVector: Float32Array,
    limit: number,
    threshold: number,
): Promise<LocalSearchResult[]> {
    await embeddingStore.open();
    // TODO: Performance — getAll() loads all embeddings into memory on every call.
    // For large stores (>1000 embeddings), consider caching or IndexedDB index-based filtering.
    const allEmbeddings = await embeddingStore.getAll();

    const results: LocalSearchResult[] = [];

    // One call per cached embedding, so the loop is timed and not each call.
    timed('ats', 'cosine_similarity_f32_loop', () => {
        for (const [sourceId, vector] of allEmbeddings) {
            try {
                const similarity = cosineSimilarity(queryVector, vector);
                if (similarity >= threshold) {
                    results.push({ attestationId: sourceId, similarity });
                }
            } catch (err) {
                // Dimension mismatch — likely different embedding model
                log.warn(SEG.WASM, `Skipping ${sourceId}: ${err instanceof Error ? err.message : String(err)}`);
            }
        }
    }, { 'wasm.calls': allEmbeddings.size });

    // Sort by similarity descending, take top N
    results.sort((a, b) => b.similarity - a.similarity);
    return results.slice(0, limit);
}

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
import { cosineSimilarity } from './qntx-wasm';
import { log, SEG } from './logger';

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
    const allEmbeddings = await embeddingStore.getAll();

    const results: LocalSearchResult[] = [];

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

    // Sort by similarity descending, take top N
    results.sort((a, b) => b.similarity - a.similarity);
    return results.slice(0, limit);
}

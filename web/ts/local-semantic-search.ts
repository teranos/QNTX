/**
 * Local semantic search — brute-force cosine similarity over IndexedDB embeddings.
 *
 * Used as offline fallback when the server is unreachable. Iterates all stored
 * embeddings, computes cosine similarity via WASM, and returns top matches.
 */

import { getAllEmbeddings } from './browser-sync';
import { semanticSimilarity } from './qntx-wasm';

/** A local search match with attestation ID and similarity score */
export interface LocalSearchResult {
    attestation_id: string;
    similarity: number;
}

/**
 * Search local embeddings against a query embedding.
 *
 * @param queryEmbedding - The query's embedding vector
 * @param threshold - Minimum similarity score (0-1)
 * @param limit - Maximum results to return
 * @returns Matches sorted by similarity descending
 */
export async function localSemanticSearch(
    queryEmbedding: number[],
    threshold: number,
    limit: number,
): Promise<LocalSearchResult[]> {
    const embeddings = await getAllEmbeddings();
    if (embeddings.length === 0) return [];

    const results: LocalSearchResult[] = [];

    for (const emb of embeddings) {
        const sim = semanticSimilarity(queryEmbedding, emb.vector);
        if (sim >= threshold) {
            results.push({
                attestation_id: emb.attestation_id,
                similarity: sim,
            });
        }
    }

    // Sort descending by similarity, take top N
    results.sort((a, b) => b.similarity - a.similarity);
    return results.slice(0, limit);
}

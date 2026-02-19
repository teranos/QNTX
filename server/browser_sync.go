package server

import (
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
)

// browserSyncBatchSize is how many attestations to send per WebSocket message.
const browserSyncBatchSize = 200

// handleBrowserSyncHello compares the browser's Merkle root with the server's,
// and streams missing attestations (with embeddings) to the browser in batches.
// Runs in a goroutine to avoid blocking the read pump.
func (c *Client) handleBrowserSyncHello(syncRoot string) {
	c.server.wg.Add(1)
	go func() {
		defer c.server.wg.Done()

		if c.server.syncTree == nil {
			c.sendJSON(BrowserSyncDoneMessage{
				Type:      "browser_sync_done",
				Message:   "sync tree unavailable",
				Timestamp: time.Now().Unix(),
			})
			return
		}

		serverRoot, err := c.server.syncTree.Root()
		if err != nil {
			c.server.logger.Warnw("Browser sync: failed to get server root",
				"client_id", c.id, "error", err)
			c.sendJSON(BrowserSyncDoneMessage{
				Type:      "browser_sync_done",
				Message:   "failed to read server Merkle root",
				Timestamp: time.Now().Unix(),
			})
			return
		}

		// Roots match — nothing to sync
		if syncRoot == serverRoot {
			c.sendJSON(BrowserSyncDoneMessage{
				Type:      "browser_sync_done",
				Message:   "roots match",
				Timestamp: time.Now().Unix(),
			})
			c.server.logger.Debugw("Browser sync: roots match, nothing to send",
				"client_id", c.id, "root", serverRoot[:12])
			return
		}

		// Get server group hashes and diff against browser
		serverGroups, err := c.server.syncTree.GroupHashes()
		if err != nil {
			c.server.logger.Warnw("Browser sync: failed to get group hashes",
				"client_id", c.id, "error", err)
			return
		}

		// Browser sends empty root — it has nothing, so all server groups are "remote_only" from browser's perspective
		// We treat the browser as having no groups (empty map), so server groups = remote_only
		browserGroups := make(map[string]string) // browser has nothing beyond its root
		_, remoteOnly, divergent, err := c.server.syncTree.Diff(browserGroups)
		if err != nil {
			c.server.logger.Warnw("Browser sync: diff failed",
				"client_id", c.id, "error", err)
			return
		}

		// Groups the browser needs = all server groups (since browser sent only root, not group hashes)
		// In practice the browser has an empty tree or a subset — send everything the server has.
		missingGroups := append(remoteOnly, divergent...)
		if len(missingGroups) == 0 {
			// If diff returns nothing (browser root differs but no groups differ),
			// send all server groups
			missingGroups = make([]string, 0, len(serverGroups))
			for gkh := range serverGroups {
				missingGroups = append(missingGroups, gkh)
			}
		}

		c.server.logger.Infow("Browser sync: starting",
			"client_id", c.id,
			"missing_groups", len(missingGroups),
			"server_root", serverRoot[:12],
			"browser_root", truncateHash(syncRoot))

		if err := c.streamAttestationsForGroups(missingGroups); err != nil {
			c.server.logger.Warnw("Browser sync: streaming failed",
				"client_id", c.id, "error", err)
		}
	}()
}

// streamAttestationsForGroups resolves group key hashes to (actor, context),
// queries attestations, looks up embeddings, and sends in batches.
func (c *Client) streamAttestationsForGroups(groupKeyHashes []string) error {
	var batch []interface{}
	var batchEmbeddings []BrowserSyncEmbedding
	var batchSourceIDs []string
	totalSent := 0
	totalAtts := 0

	// First pass: count total attestations for progress reporting
	for _, gkh := range groupKeyHashes {
		actor, ctx, err := c.server.syncTree.FindGroupKey(gkh)
		if err != nil {
			continue
		}
		atts, err := storage.GetAttestations(c.server.db, ats.AttestationFilter{
			Actors:   []string{actor},
			Contexts: []string{ctx},
		})
		if err != nil {
			continue
		}
		totalAtts += len(atts)
	}

	// Second pass: stream attestations in batches
	for _, gkh := range groupKeyHashes {
		actor, ctx, err := c.server.syncTree.FindGroupKey(gkh)
		if err != nil {
			continue
		}

		atts, err := storage.GetAttestations(c.server.db, ats.AttestationFilter{
			Actors:   []string{actor},
			Contexts: []string{ctx},
		})
		if err != nil {
			c.server.logger.Debugw("Browser sync: query failed for group",
				"actor", actor, "context", ctx, "error", err)
			continue
		}

		for _, att := range atts {
			protoAtt := toProtoMap(att)
			batch = append(batch, protoAtt)
			batchSourceIDs = append(batchSourceIDs, att.ID)

			if len(batch) >= browserSyncBatchSize {
				embs := c.lookupEmbeddings(batchSourceIDs)
				totalSent += len(batch)
				c.sendSyncBatch(batch, embs, totalSent, totalAtts, false)
				batch = batch[:0]
				batchEmbeddings = batchEmbeddings[:0]
				batchSourceIDs = batchSourceIDs[:0]
			}
		}
	}

	// Final batch (with done=true)
	if len(batch) > 0 {
		embs := c.lookupEmbeddings(batchSourceIDs)
		totalSent += len(batch)
		c.sendSyncBatch(batch, embs, totalSent, totalAtts, true)
	} else {
		// No remaining items but still need to signal done
		c.sendSyncBatch(nil, nil, totalSent, totalAtts, true)
	}

	c.server.logger.Infow("Browser sync: complete",
		"client_id", c.id,
		"attestations_sent", totalSent)

	return nil
}

// lookupEmbeddings fetches embeddings for a slice of attestation IDs.
func (c *Client) lookupEmbeddings(sourceIDs []string) []BrowserSyncEmbedding {
	if c.server.embeddingStore == nil || c.server.embeddingService == nil || len(sourceIDs) == 0 {
		return nil
	}

	embMap, err := c.server.embeddingStore.GetBySourceIDs("attestation", sourceIDs)
	if err != nil {
		c.server.logger.Debugw("Browser sync: embedding lookup failed", "error", err)
		return nil
	}

	result := make([]BrowserSyncEmbedding, 0, len(embMap))
	for sourceID, emb := range embMap {
		vec, err := c.server.embeddingService.DeserializeEmbedding(emb.Embedding)
		if err != nil {
			continue
		}
		result = append(result, BrowserSyncEmbedding{
			AttestationID: sourceID,
			Vector:        vec,
			Model:         emb.Model,
		})
	}
	return result
}

// sendSyncBatch sends a BrowserSyncAttestationsMessage to the client.
func (c *Client) sendSyncBatch(attestations []interface{}, embeddings []BrowserSyncEmbedding, stored, total int, done bool) {
	if attestations == nil {
		attestations = make([]interface{}, 0)
	}
	if embeddings == nil {
		embeddings = make([]BrowserSyncEmbedding, 0)
	}
	c.sendJSON(BrowserSyncAttestationsMessage{
		Type:         "browser_sync_attestations",
		Attestations: attestations,
		Embeddings:   embeddings,
		Done:         done,
		Stored:       stored,
		Total:        total,
		Timestamp:    time.Now().Unix(),
	})
}

// toProtoMap converts a types.As to a map matching proto Attestation JSON format.
// Timestamps become Unix millis (i64), attributes become a JSON string.
func toProtoMap(att *types.As) map[string]interface{} {
	m := map[string]interface{}{
		"id":         att.ID,
		"subjects":   att.Subjects,
		"predicates": att.Predicates,
		"contexts":   att.Contexts,
		"actors":     att.Actors,
		"timestamp":  att.Timestamp.UnixMilli(),
		"source":     att.Source,
		"created_at": att.CreatedAt.UnixMilli(),
	}
	if att.Attributes != nil {
		attrJSON, err := json.Marshal(att.Attributes)
		if err == nil {
			m["attributes"] = string(attrJSON)
		}
	}
	return m
}

// sendQueryEmbeddingToBrowser generates an embedding for the SE query and sends it
// to the browser for offline semantic search. Runs in a goroutine; no-op if embedding
// service is unavailable.
func (c *Client) sendQueryEmbeddingToBrowser(watcherID, query string) {
	if c.server.embeddingService == nil {
		return
	}
	c.server.wg.Add(1)
	go func() {
		defer c.server.wg.Done()
		result, err := c.server.embeddingService.GenerateEmbedding(query)
		if err != nil {
			c.server.logger.Debugw("Failed to generate SE query embedding for browser",
				"watcher_id", watcherID, "error", err)
			return
		}
		c.sendJSON(SEQueryEmbeddingMessage{
			Type:      "se_query_embedding",
			WatcherID: watcherID,
			Embedding: result.Embedding,
			Timestamp: time.Now().Unix(),
		})
	}()
}

// truncateHash returns first 12 chars of a hash for logging, or the full string if shorter.
func truncateHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}


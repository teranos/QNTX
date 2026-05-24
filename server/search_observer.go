package server

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// SearchIndexObserver pushes attestations with rich text fields into MeiliSearch
// on creation. Implements storage.AttestationObserver — called asynchronously
// by NotifyObservers, so errors are logged but don't block attestation creation.
//
// Only indexes attestations whose type declares rich_string_fields.
// Documents are indexed into the "attestations" MeiliSearch index.
type SearchIndexObserver struct {
	servicesManager *grpcplugin.ServicesManager
	richStore       *storage.BoundedStore
	logger          *zap.SugaredLogger
	indexed         atomic.Int64
	indexConfigured atomic.Bool
}

// NewSearchIndexObserver creates a search indexing observer.
func NewSearchIndexObserver(
	sm *grpcplugin.ServicesManager,
	richStore *storage.BoundedStore,
	logger *zap.SugaredLogger,
) *SearchIndexObserver {
	return &SearchIndexObserver{
		servicesManager: sm,
		richStore:       richStore,
		logger:          logger,
	}
}

// ensureIndex configures the MeiliSearch "attestations" index on first use.
func (o *SearchIndexObserver) ensureIndex(router *grpcplugin.SearchServer) {
	if o.indexConfigured.Load() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := router.ConfigureIndex(ctx, &protocol.ConfigureIndexRequest{
		Index:      "attestations",
		PrimaryKey: "id",
		SearchableAttributes: []string{
			"field_value",
			"display_label",
			"type_name",
			"node_id",
		},
		FilterableAttributes: []string{
			"type_name",
			"field_name",
			"node_id",
			"actor",
			"context",
		},
		SortableAttributes: []string{},
	})
	if err != nil {
		o.logger.Warnw("Failed to configure MeiliSearch attestations index", "error", err)
		return
	}

	o.indexConfigured.Store(true)
	o.logger.Infow("MeiliSearch attestations index configured")
}

// OnAttestationCreated indexes the attestation into MeiliSearch if it has rich text.
func (o *SearchIndexObserver) OnAttestationCreated(as *types.As) {
	router := o.servicesManager.GetSearchRouter()
	if router == nil || !router.HasProvider() {
		return
	}

	doc := o.buildDocument(as)
	if doc == nil {
		return
	}

	o.ensureIndex(router)

	docJSON, err := json.Marshal(doc)
	if err != nil {
		o.logger.Warnw("Failed to marshal search document",
			"attestation_id", as.ID,
			"error", err,
		)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = router.IndexDocuments(ctx, &protocol.IndexDocumentsRequest{
		Index:     "attestations",
		Documents: [][]byte{docJSON},
	})
	if err != nil {
		o.logger.Warnw("Failed to index attestation in MeiliSearch",
			"attestation_id", as.ID,
			"error", err,
		)
		return
	}

	o.indexed.Add(1)
}

// buildDocument creates a MeiliSearch document from an attestation.
// Returns nil if the attestation has no rich text fields to index.
func (o *SearchIndexObserver) buildDocument(as *types.As) map[string]interface{} {
	if as.Attributes == nil || len(as.Attributes) == 0 {
		return nil
	}

	richFields := o.richStore.GetDiscoveredRichFields()
	if len(richFields) == 0 {
		return nil
	}

	// Check if this attestation has any rich text content
	var hasRichContent bool
	for _, field := range richFields {
		if val, ok := as.Attributes[field]; ok {
			if str, ok := val.(string); ok && str != "" {
				hasRichContent = true
				break
			}
		}
	}
	if !hasRichContent {
		return nil
	}

	// Build the document — include all attributes plus structural fields
	doc := make(map[string]interface{})
	doc["id"] = as.ID

	// Structural fields for filtering and display
	if len(as.Subjects) > 0 {
		doc["node_id"] = as.Subjects[0]
	}
	if len(as.Predicates) > 0 {
		doc["predicate"] = as.Predicates[0]
	}
	if len(as.Contexts) > 0 {
		doc["context"] = as.Contexts[0]
	}
	if len(as.Actors) > 0 {
		doc["actor"] = as.Actors[0]
	}

	// Type metadata from attributes
	typeName, _ := as.Attributes["display_label"].(string)
	if typeName == "" {
		typeName, _ = as.Attributes["type"].(string)
	}
	doc["type_name"] = typeName
	doc["type_label"] = typeName
	doc["display_label"] = typeName

	// Rich text fields — these are what MeiliSearch actually searches
	for _, field := range richFields {
		if val, ok := as.Attributes[field]; ok {
			doc["field_name"] = field
			if str, ok := val.(string); ok {
				doc["field_value"] = str
			}
		}
	}

	// All attributes for full context in search results
	for k, v := range as.Attributes {
		if _, exists := doc[k]; !exists {
			doc[k] = v
		}
	}

	return doc
}

// DrainIndexedCount atomically reads and resets the indexed counter.
func (o *SearchIndexObserver) DrainIndexedCount() int {
	return int(o.indexed.Swap(0))
}

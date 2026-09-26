package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/watcher"
	elementstorage "github.com/teranos/QNTX/element/storage"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/sym"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// CanvasHandler handles HTTP requests for canvas state
type CanvasHandler struct {
	store *elementstorage.CanvasStore
	// canvasFor is the canvas store of the namespace a request acts in. Nil
	// means store, for a node with one namespace.
	canvasFor     func(*http.Request) (*elementstorage.CanvasStore, error)
	people        People
	mailer        Mailer
	inviteLink    func(token string) string
	watcherEngine *watcher.Engine
	logger        *zap.SugaredLogger
	serverPort    int // Server port for internal plugin calls
}

// NewCanvasHandler creates a new canvas handler
func NewCanvasHandler(store *elementstorage.CanvasStore, opts ...CanvasHandlerOption) *CanvasHandler {
	h := &CanvasHandler{
		store: store,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// CanvasHandlerOption configures optional dependencies for the canvas handler
type CanvasHandlerOption func(*CanvasHandler)

// WithWatcherEngine enables meld edge subscription compilation
func WithWatcherEngine(engine *watcher.Engine, logger *zap.SugaredLogger) CanvasHandlerOption {
	return func(h *CanvasHandler) {
		h.watcherEngine = engine
		h.logger = logger
	}
}

// WithCanvasFor resolves the canvas store per request, from the namespace the
// request acts in. A canvas lives in one namespace and only that one (ADR-026).
func WithCanvasFor(canvasFor func(*http.Request) (*elementstorage.CanvasStore, error)) CanvasHandlerOption {
	return func(h *CanvasHandler) {
		h.canvasFor = canvasFor
	}
}

// storeOf is the store of the canvas a request acts on: the one `canvas`
// names, or the namespace's. The caller has to own it, or have been granted
// it, or own every canvas (ROOT, and SUPER). What it cannot give, it has
// already answered on w.
func (h *CanvasHandler) storeOf(w http.ResponseWriter, r *http.Request) (*elementstorage.CanvasStore, bool) {
	store, ok := h.anyStoreOf(w, r)
	if !ok {
		return nil, false
	}
	// A node with one namespace serves default, and default has its canvas.
	if h.canvasFor == nil {
		return store, true
	}
	canvas, err := h.canvasNamed(r, store)
	if err != nil {
		h.writeCanvasError(w, err)
		return nil, false
	}
	if !h.mayAct(r, canvas) {
		h.writeError(w, errors.Newf("the canvas %s is not yours", canvas.ID), http.StatusForbidden)
		return nil, false
	}
	return store.In(canvas.ID), true
}

// canvasNamed is the canvas a request names with `canvas`, or the
// namespace's own when it names none.
func (h *CanvasHandler) canvasNamed(r *http.Request, store *elementstorage.CanvasStore) (elementstorage.Canvas, error) {
	if id := r.URL.Query().Get("canvas"); id != "" {
		return store.Canvas(r.Context(), id)
	}
	canvases, err := store.Canvases(r.Context())
	if err != nil {
		return elementstorage.Canvas{}, err
	}
	for _, c := range canvases {
		if c.Kind == elementstorage.CanvasOfTheNamespace {
			return c, nil
		}
	}
	return elementstorage.Canvas{}, elementstorage.ErrNoCanvas
}

// mayAct is whether the caller may act on a canvas: its owners may, those
// granted the namespace's canvas may, and ROOT and SUPER own every one.
func (h *CanvasHandler) mayAct(r *http.Request, canvas elementstorage.Canvas) bool {
	admitted, gated := auth.AdmissionFrom(r.Context())
	if !gated || admitted.OwnsEveryCanvas() {
		return true
	}
	return slices.Contains(canvas.Owners, admitted.UserID) ||
		(canvas.Kind == elementstorage.CanvasOfTheNamespace && slices.Contains(canvas.Access, admitted.UserID))
}

// anyStoreOf is the store of the namespace a request acts in, whether or not
// its canvas was created.
func (h *CanvasHandler) anyStoreOf(w http.ResponseWriter, r *http.Request) (*elementstorage.CanvasStore, bool) {
	if h.canvasFor == nil {
		return h.store, true
	}
	store, err := h.canvasFor(r)
	if err != nil {
		h.writeCanvasError(w, err)
		return nil, false
	}
	return store, true
}

// writeCanvasError answers a namespace with no canvas with 404, and one the
// caller cannot act in with 403.
func (h *CanvasHandler) writeCanvasError(w http.ResponseWriter, err error) {
	if errors.Is(err, elementstorage.ErrNoCanvas) {
		h.writeError(w, err, http.StatusNotFound)
		return
	}
	h.writeError(w, err, http.StatusForbidden)
}

// HandleCanvas answers what the canvas of this namespace is, and creates it.
// Routes:
//
//	GET  /api/canvas  - {"name": ...}, or 404 when there is no canvas
//	POST /api/canvas  - {"name": ...} creates it; 409 when there is one
func (h *CanvasHandler) HandleCanvas(w http.ResponseWriter, r *http.Request) {
	store, ok := h.anyStoreOf(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		name, err := store.Name(r.Context())
		if err != nil {
			if errors.Is(err, elementstorage.ErrNoCanvas) {
				h.writeError(w, err, http.StatusNotFound)
			} else {
				h.writeError(w, err, http.StatusInternalServerError)
			}
			return
		}
		h.writeJSON(w, map[string]string{"name": name})
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
			return
		}
		if body.Name == "" {
			h.writeError(w, errors.New("name is required"), http.StatusBadRequest)
			return
		}
		admitted, gated := auth.AdmissionFrom(r.Context())
		if gated && !admitted.OwnsEveryCanvas() {
			h.writeError(w, errors.New("only ROOT, or SUPER here, creates the namespace's canvas"), http.StatusForbidden)
			return
		}
		if err := store.Create(r.Context(), body.Name, admitted.UserID); err != nil {
			if errors.Is(err, elementstorage.ErrCanvasExists) {
				h.writeError(w, err, http.StatusConflict)
			} else {
				h.writeError(w, err, http.StatusInternalServerError)
			}
			return
		}
		w.WriteHeader(http.StatusCreated)
		h.writeJSON(w, map[string]string{"name": body.Name})
	default:
		h.writeError(w, errors.NewMethodNotAllowedError(r.Method), http.StatusMethodNotAllowed)
	}
}

// WithServerPort sets the server port for internal plugin calls
func WithServerPort(port int) CanvasHandlerOption {
	return func(h *CanvasHandler) {
		h.serverPort = port
	}
}

// HandleElements handles element CRUD operations
// Routes:
//
//	GET    /api/canvas/elements       - List all elements
//	POST   /api/canvas/elements       - Create/update an element
//	GET    /api/canvas/elements/{id}  - Get an element by ID
//	DELETE /api/canvas/elements/{id}  - Delete an element
func (h *CanvasHandler) HandleElements(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path if present
	path := strings.TrimPrefix(r.URL.Path, "/api/canvas/elements")
	path = strings.TrimPrefix(path, "/")
	elementID := path

	switch r.Method {
	case http.MethodGet:
		if elementID == "" {
			h.handleListElements(w, r)
		} else {
			h.handleGetElement(w, r, elementID)
		}
	case http.MethodPost:
		h.handleUpsertElement(w, r)
	case http.MethodDelete:
		if elementID == "" {
			h.writeError(w, errors.New("element ID required for delete"), http.StatusBadRequest)
			return
		}
		h.handleDeleteElement(w, r, elementID)
	default:
		h.writeError(w, errors.NewMethodNotAllowedError(r.Method), http.StatusMethodNotAllowed)
	}
}

// HandleCompositions handles composition CRUD operations
// Routes:
//
//	GET    /api/canvas/compositions       - List all compositions
//	POST   /api/canvas/compositions       - Create/update a composition
//	GET    /api/canvas/compositions/{id}  - Get a composition by ID
//	DELETE /api/canvas/compositions/{id}  - Delete a composition
func (h *CanvasHandler) HandleCompositions(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path if present
	path := strings.TrimPrefix(r.URL.Path, "/api/canvas/compositions")
	path = strings.TrimPrefix(path, "/")
	compID := path

	switch r.Method {
	case http.MethodGet:
		if compID == "" {
			h.handleListCompositions(w, r)
		} else {
			h.handleGetComposition(w, r, compID)
		}
	case http.MethodPost:
		h.handleUpsertComposition(w, r)
	case http.MethodDelete:
		if compID == "" {
			h.writeError(w, errors.New("composition ID required for delete"), http.StatusBadRequest)
			return
		}
		h.handleDeleteComposition(w, r, compID)
	default:
		h.writeError(w, errors.NewMethodNotAllowedError(r.Method), http.StatusMethodNotAllowed)
	}
}

// === Element handlers ===

func (h *CanvasHandler) handleListElements(w http.ResponseWriter, r *http.Request) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	items, err := store.ListElements(r.Context())
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []*elementstorage.CanvasElement{}
	}

	h.writeJSON(w, items)
}

func (h *CanvasHandler) handleGetElement(w http.ResponseWriter, r *http.Request, id string) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	item, err := store.GetElement(r.Context(), id)
	if err != nil {
		if errors.Is(err, elementstorage.ErrNotFound) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	h.writeJSON(w, item)
}

func (h *CanvasHandler) handleUpsertElement(w http.ResponseWriter, r *http.Request) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	var item elementstorage.CanvasElement
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}

	if err := store.UpsertElement(r.Context(), &item); err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, item)
}

func (h *CanvasHandler) handleDeleteElement(w http.ResponseWriter, r *http.Request, id string) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	if err := store.DeleteElement(r.Context(), id); err != nil {
		if errors.Is(err, elementstorage.ErrNotFound) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// === Composition handlers ===

func (h *CanvasHandler) handleListCompositions(w http.ResponseWriter, r *http.Request) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	comps, err := store.ListCompositions(r.Context())
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}
	if comps == nil {
		comps = []*elementstorage.CanvasComposition{}
	}

	h.writeJSON(w, comps)
}

func (h *CanvasHandler) handleGetComposition(w http.ResponseWriter, r *http.Request, id string) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	comp, err := store.GetComposition(r.Context(), id)
	if err != nil {
		if errors.Is(err, elementstorage.ErrNotFound) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	h.writeJSON(w, comp)
}

func (h *CanvasHandler) handleUpsertComposition(w http.ResponseWriter, r *http.Request) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	var comp elementstorage.CanvasComposition
	if err := json.NewDecoder(r.Body).Decode(&comp); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}

	if err := store.UpsertComposition(r.Context(), &comp); err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}

	// Compile meld edges into watcher subscriptions
	if h.watcherEngine != nil {
		if err := h.compileSubscriptions(r.Context(), store, &comp); err != nil {
			h.logWarn("Failed to compile subscriptions for composition %s: %v", comp.ID, err)
			// Non-fatal: composition is stored, subscriptions can be retried
		}
	}

	h.writeJSON(w, comp)
}

func (h *CanvasHandler) handleDeleteComposition(w http.ResponseWriter, r *http.Request, id string) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	// Re-enable downstream SE watchers that were disabled by SE→SE meld edges
	if h.watcherEngine != nil {
		h.reEnableDownstreamSEWatchers(r.Context(), id)
	}

	// Remove meld edge subscriptions before deleting composition
	if h.watcherEngine != nil {
		prefix := fmt.Sprintf("meld-edge-%s-", id)
		if n, err := h.watcherEngine.GetStore().DeleteByPrefix(r.Context(), prefix); err != nil {
			h.logWarn("Failed to delete meld edge watchers for composition %s: %v", id, err)
		} else if n > 0 {
			h.logInfo("Deleted %d meld edge watchers for composition %s", n, id)
			if err := h.watcherEngine.ReloadWatchers(); err != nil {
				h.logWarn("Failed to reload watchers after composition %s delete: %v", id, err)
			}
		}
	}

	// Cascade delete edge cursors
	if h.watcherEngine != nil {
		if _, err := h.watcherEngine.DB().ExecContext(r.Context(),
			"DELETE FROM composition_edge_cursors WHERE composition_id = ?", id); err != nil {
			h.logWarn("Failed to delete edge cursors for composition %s: %v", id, err)
		}
	}

	if err := store.DeleteComposition(r.Context(), id); err != nil {
		if errors.Is(err, elementstorage.ErrNotFound) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleMinimizedWindows handles minimized window CRUD operations
// Routes:
//
//	GET    /api/canvas/minimized-windows       - List all minimized windows
//	POST   /api/canvas/minimized-windows       - Add a minimized window
//	DELETE /api/canvas/minimized-windows/{id}  - Remove a minimized window
func (h *CanvasHandler) HandleMinimizedWindows(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/canvas/minimized-windows")
	path = strings.TrimPrefix(path, "/")
	elementID := path

	switch r.Method {
	case http.MethodGet:
		h.handleListMinimizedWindows(w, r)
	case http.MethodPost:
		h.handleAddMinimizedWindow(w, r)
	case http.MethodDelete:
		if elementID == "" {
			h.writeError(w, errors.New("element ID required for delete"), http.StatusBadRequest)
			return
		}
		h.handleRemoveMinimizedWindow(w, r, elementID)
	default:
		h.writeError(w, errors.NewMethodNotAllowedError(r.Method), http.StatusMethodNotAllowed)
	}
}

func (h *CanvasHandler) handleListMinimizedWindows(w http.ResponseWriter, r *http.Request) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	windows, err := store.ListMinimizedWindows(r.Context())
	if err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}
	if windows == nil {
		windows = []*elementstorage.MinimizedWindow{}
	}

	h.writeJSON(w, windows)
}

func (h *CanvasHandler) handleAddMinimizedWindow(w http.ResponseWriter, r *http.Request) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	var body struct {
		ElementID string `json:"element_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}

	if body.ElementID == "" {
		h.writeError(w, errors.New("element_id is required"), http.StatusBadRequest)
		return
	}

	if err := store.AddMinimizedWindow(r.Context(), body.ElementID); err != nil {
		h.writeError(w, err, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (h *CanvasHandler) handleRemoveMinimizedWindow(w http.ResponseWriter, r *http.Request, elementID string) {
	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	if err := store.RemoveMinimizedWindow(r.Context(), elementID); err != nil {
		if errors.Is(err, elementstorage.ErrNotFound) {
			h.writeError(w, err, http.StatusNotFound)
		} else {
			h.writeError(w, err, http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// === Subscription compilation ===

// compileSubscriptions converts a composition's right-direction edges into watcher subscriptions.
// AX source edges use the AX element's query filter. Producer (py/prompt) source edges filter on actor.
func (h *CanvasHandler) compileSubscriptions(ctx context.Context, canvas *elementstorage.CanvasStore, comp *elementstorage.CanvasComposition) error {
	store := h.watcherEngine.GetStore()

	// Re-enable any SE watchers disabled by previous compilation,
	// then delete stale meld-edge watchers. This ensures removed edges
	// don't leave downstream SE watchers permanently disabled.
	h.reEnableDownstreamSEWatchers(ctx, comp.ID)

	prefix := fmt.Sprintf("meld-edge-%s-", comp.ID)
	deleted, err := store.DeleteByPrefix(ctx, prefix)
	if err != nil {
		return errors.Wrapf(err, "failed to clean stale watchers for composition %s", comp.ID)
	}

	var created int
	for _, edge := range comp.Edges {
		if edge.Direction != "right" {
			continue
		}

		// Resolve source element type
		sourceElement, err := canvas.GetElement(ctx, edge.From)
		if err != nil {
			h.logWarn("Skipping edge %s→%s: failed to resolve source element: %v", edge.From, edge.To, err)
			continue
		}

		// Resolve target element type
		targetElement, err := canvas.GetElement(ctx, edge.To)
		if err != nil {
			h.logWarn("Skipping edge %s→%s: failed to resolve target element: %v", edge.From, edge.To, err)
			continue
		}

		// Resolve element types
		targetType := elementSymbolToType(targetElement.Symbol)
		sourceType := elementSymbolToType(sourceElement.Symbol)

		// SE → SE: compound semantic watcher (intersection)
		if sourceType == "semantic" && targetType == "semantic" {
			watcherID := fmt.Sprintf("meld-edge-%s-%s-%s", comp.ID, edge.From, edge.To)

			// Get upstream (source) SE element's query
			upstreamWatcherID := fmt.Sprintf("se-element-%s", edge.From)
			upstreamWatcher, err := store.Get(ctx, upstreamWatcherID)
			if err != nil {
				h.logWarn("Skipping SE→SE edge %s→%s: no watcher found for upstream %s: %v", edge.From, edge.To, upstreamWatcherID, err)
				continue
			}

			// Get downstream (target) SE element's query
			downstreamWatcherID := fmt.Sprintf("se-element-%s", edge.To)
			downstreamWatcher, err := store.Get(ctx, downstreamWatcherID)
			if err != nil {
				h.logWarn("Skipping SE→SE edge %s→%s: no watcher found for downstream %s: %v", edge.From, edge.To, downstreamWatcherID, err)
				continue
			}

			actionData, err := json.Marshal(map[string]string{
				"target_element_id": edge.To,
				"composition_id":    comp.ID,
				"source_element_id": edge.From,
			})
			if err != nil {
				return errors.Wrap(err, "failed to marshal action data")
			}

			w := &storage.Watcher{
				ID:                        watcherID,
				Name:                      fmt.Sprintf("Meld: %s → %s", edge.From, edge.To),
				ActionType:                storage.ActionTypeSemanticMatch,
				ActionData:                string(actionData),
				MaxFiresPerSecond:         1,
				Enabled:                   true,
				SemanticQuery:             downstreamWatcher.SemanticQuery,
				SemanticThreshold:         downstreamWatcher.SemanticThreshold,
				SemanticClusterID:         downstreamWatcher.SemanticClusterID,
				UpstreamSemanticQuery:     upstreamWatcher.SemanticQuery,
				UpstreamSemanticThreshold: upstreamWatcher.SemanticThreshold,
			}

			if err := store.CreateOrReplace(ctx, w); err != nil {
				return errors.Wrapf(err, "failed to create SE→SE subscription for edge %s→%s", edge.From, edge.To)
			}

			// Engine-level suppression in loadWatchers handles removing the
			// standalone SE watcher from the in-memory map. No DB disable needed.

			created++
			continue
		}

		// Target must be an executable element type for non-SE targets
		if targetType != "py" && targetType != "prompt" {
			continue
		}

		watcherID := fmt.Sprintf("meld-edge-%s-%s-%s", comp.ID, edge.From, edge.To)
		actionData, err := json.Marshal(map[string]string{
			"target_element_id":   edge.To,
			"target_element_type": targetType,
			"composition_id":      comp.ID,
			"source_element_id":   edge.From,
		})
		if err != nil {
			return errors.Wrap(err, "failed to marshal action data")
		}

		w := &storage.Watcher{
			ID:                watcherID,
			Name:              fmt.Sprintf("Meld: %s → %s", edge.From, edge.To),
			ActionType:        storage.ActionTypeElementExecute,
			ActionData:        string(actionData),
			MaxFiresPerSecond: 1,
			Enabled:           true,
		}

		// Set filter based on source element type
		switch sourceType {
		case "ax":
			// AX source: reuse the AX element's query from its existing watcher
			axWatcherID := fmt.Sprintf("ax-element-%s", edge.From)
			axWatcher, err := store.Get(ctx, axWatcherID)
			if err != nil {
				h.logWarn("Skipping AX edge %s→%s: no watcher found for %s: %v", edge.From, edge.To, axWatcherID, err)
				continue
			}
			w.AxQuery = axWatcher.AxQuery
		case "semantic":
			// Semantic source → executable target: reuse the ⊨ element's semantic query
			seWatcherID := fmt.Sprintf("se-element-%s", edge.From)
			seWatcher, err := store.Get(ctx, seWatcherID)
			if err != nil {
				h.logWarn("Skipping semantic edge %s→%s: no watcher found for %s: %v", edge.From, edge.To, seWatcherID, err)
				continue
			}
			w.SemanticQuery = seWatcher.SemanticQuery
			w.SemanticThreshold = seWatcher.SemanticThreshold

			// If source SE is itself a downstream target of an SE→SE meld,
			// propagate the upstream query so the engine enforces the full
			// intersection before executing the downstream element.
			compoundWatchers, err := store.FindCompoundWatchersForTarget(ctx, edge.From)
			if err == nil && len(compoundWatchers) > 0 {
				w.UpstreamSemanticQuery = compoundWatchers[0].UpstreamSemanticQuery
				w.UpstreamSemanticThreshold = compoundWatchers[0].UpstreamSemanticThreshold
			}
		case "py", "prompt":
			// Producer source: filter on attestations created by the upstream element
			w.Filter.Actors = []string{fmt.Sprintf("element:%s", edge.From)}
		default:
			h.logWarn("Skipping edge %s→%s: unsupported source type %s", edge.From, edge.To, sourceType)
			continue
		}

		if err := store.CreateOrReplace(ctx, w); err != nil {
			return errors.Wrapf(err, "failed to create subscription for edge %s→%s", edge.From, edge.To)
		}
		created++
	}

	if created > 0 || deleted > 0 {
		if err := h.watcherEngine.ReloadWatchers(); err != nil {
			return errors.Wrap(err, "failed to reload watchers after subscription compilation")
		}
		h.logInfo("Compiled %d subscriptions for composition %s (cleaned %d stale)", created, comp.ID, deleted)
	}

	return nil
}

// reEnableDownstreamSEWatchers logs which SE watchers will be restored when a
// composition containing SE→SE meld edges is deleted. The actual restoration
// happens via ReloadWatchers() after the compound meld-edge watcher is deleted
// from the DB — the suppression loop in loadWatchers no longer finds a compound
// watcher targeting the SE element, so the standalone SE watcher re-enters the
// in-memory map naturally. No DB update is needed because engine-level
// suppression never sets Enabled=false in the DB.
func (h *CanvasHandler) reEnableDownstreamSEWatchers(ctx context.Context, compositionID string) {
	store := h.watcherEngine.GetStore()
	watchers, err := store.List(ctx, false)
	if err != nil {
		h.logWarn("Failed to list watchers for SE re-enable: %v", err)
		return
	}

	prefix := fmt.Sprintf("meld-edge-%s-", compositionID)
	for _, w := range watchers {
		if w.ActionType != storage.ActionTypeSemanticMatch || !strings.HasPrefix(w.ID, prefix) {
			continue
		}
		var actionData struct {
			TargetElementID string `json:"target_element_id"`
		}
		if err := json.Unmarshal([]byte(w.ActionData), &actionData); err != nil {
			h.logWarn("Failed to unmarshal action data for meld-edge watcher %s: %v", w.ID, err)
			continue
		}
		if actionData.TargetElementID == "" {
			continue
		}
		h.logInfo("SE watcher se-element-%s will be restored after unmeld (compound %s removed)", actionData.TargetElementID, w.ID)
	}
}

// elementSymbolToType maps element symbol to short type name for subscription logic.
// Symbols come from the sym package or are stored as literal strings (e.g. "py").
func elementSymbolToType(symbol string) string {
	switch symbol {
	case "py":
		return "py"
	case sym.AX: // ⋈
		return "ax"
	case sym.SE: // ⊨ — semantic search element
		return "semantic"
	case sym.SO: // ⟶ — prompt element uses SO symbol
		return "prompt"
	default:
		return symbol
	}
}

// === Helper methods ===

func (h *CanvasHandler) logInfo(format string, args ...any) {
	if h.logger != nil {
		h.logger.Infof(format, args...)
	}
}

func (h *CanvasHandler) logWarn(format string, args ...any) {
	if h.logger != nil {
		h.logger.Warnf(format, args...)
	}
}

// HandleExportDOM receives rendered DOM HTML from client and writes to docs/demo/index.html
// POST /api/canvas/export-dom — requires html in JSON body, demo mode only
func (h *CanvasHandler) HandleExportDOM(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("QNTX_DEMO") != "1" {
		h.writeError(w, errors.New("canvas export only available in demo mode (make demo)"), http.StatusForbidden)
		return
	}

	if r.Method != http.MethodPost {
		h.writeError(w, errors.NewMethodNotAllowedError(r.Method), http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, errors.Wrap(err, "invalid request body"), http.StatusBadRequest)
		return
	}
	if body.HTML == "" {
		h.writeError(w, errors.New("html is required"), http.StatusBadRequest)
		return
	}

	// Write to docs/demo/index.html
	demoPath := filepath.Join("docs", "demo", "index.html")
	if err := os.MkdirAll(filepath.Dir(demoPath), 0755); err != nil {
		h.writeError(w, errors.Wrapf(err, "failed to create demo directory"), http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(demoPath, []byte(body.HTML), 0644); err != nil {
		h.writeError(w, errors.Wrapf(err, "failed to write %s", demoPath), http.StatusInternalServerError)
		return
	}

	h.writeJSON(w, map[string]string{
		"path": demoPath,
	})
}

// HandleExportStatic renders canvas server-side via canvas-renderer plugin
// GET /api/canvas/export?canvas_id={id} — triggers HTML download (demo mode only)
//
// Known limitations:
//   - Only exports elements with canvas_id set. Elements created before 2026-02-26
//     (when canvas_id sync was fixed) have empty canvas_id and won't export.
//   - Export quality issues: static HTML output differs from live canvas (root cause TBD).
//
// TODO: Add test coverage (canvas_export_test.go doesn't exist)
// TODO: Improve export quality - investigate rendering differences
// TODO: Migration script to backfill canvas_id for old elements (if frontend has data)
func (h *CanvasHandler) HandleExportStatic(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("QNTX_DEMO") != "1" {
		h.writeError(w, errors.New("canvas export only available in demo mode (make demo)"), http.StatusForbidden)
		return
	}

	if r.Method != http.MethodGet {
		h.writeError(w, errors.NewMethodNotAllowedError(r.Method), http.StatusMethodNotAllowed)
		return
	}

	canvasID := r.URL.Query().Get("canvas_id")
	if canvasID == "" {
		h.writeError(w, errors.New("canvas_id query parameter required"), http.StatusBadRequest)
		return
	}

	store, ok := h.storeOf(w, r)
	if !ok {
		return
	}
	// Fetch all elements and filter by canvas_id
	allElements, err := store.ListElements(r.Context())
	if err != nil {
		h.writeError(w, errors.Wrapf(err, "failed to fetch elements"), http.StatusInternalServerError)
		return
	}

	// Log canvas_id distribution for debugging
	h.logInfo("Export: found %d total elements in database", len(allElements))
	canvasIdCounts := make(map[string]int)
	for _, g := range allElements {
		canvasIdCounts[g.CanvasID]++
	}
	for cid, count := range canvasIdCounts {
		if cid == "" {
			h.logInfo("  %d elements with canvas_id=\"\" (root canvas)", count)
		} else {
			h.logInfo("  %d elements with canvas_id=%q", count, cid)
		}
	}

	// Filter to only elements that belong to this canvas
	var items []any
	for _, g := range allElements {
		if g.CanvasID == canvasID {
			items = append(items, g)
		}
	}

	// Check if we found any elements for this canvas
	h.logInfo("Export: filtered to %d elements for canvas_id=%q", len(items), canvasID)
	if len(items) == 0 {
		h.writeError(w, errors.New(fmt.Sprintf("no elements found for canvas %s (found %d total elements in database, none with canvas_id=%q)", canvasID, len(allElements), canvasID)), http.StatusNotFound)
		return
	}

	// Build request payload for canvas-renderer plugin
	pluginReq := map[string]any{
		"canvas_id": canvasID,
		"elements":  items,
	}
	reqBody, err := json.Marshal(pluginReq)
	if err != nil {
		h.writeError(w, errors.Wrap(err, "failed to marshal plugin request"), http.StatusInternalServerError)
		return
	}

	// Call canvas-renderer plugin endpoint (internal HTTP request)
	// Plugin is mounted at /api/canvas-renderer/, endpoint is /render
	pluginURL := fmt.Sprintf("http://localhost:%d/api/canvas-renderer/render", h.getServerPort())

	// Use request context for timeout/cancellation propagation
	req, err := http.NewRequestWithContext(r.Context(), "POST", pluginURL, strings.NewReader(string(reqBody)))
	if err != nil {
		h.writeError(w, errors.Wrap(err, "failed to create plugin request"), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.writeError(w, errors.Wrapf(err, "failed to call canvas-renderer plugin at %s", pluginURL), http.StatusBadGateway)
		return
	}
	defer func() { sqlclose.Log(resp.Body.Close(), h.logger, "the canvas-renderer response body") }()

	if resp.StatusCode != http.StatusOK {
		h.writeError(w, errors.New(fmt.Sprintf("canvas-renderer returned status %d", resp.StatusCode)), http.StatusBadGateway)
		return
	}

	// Parse plugin response
	var pluginResp struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pluginResp); err != nil {
		h.writeError(w, errors.Wrap(err, "failed to parse plugin response"), http.StatusInternalServerError)
		return
	}

	// Return HTML with download headers
	filename := fmt.Sprintf("canvas-%s.html", canvasID)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	if _, err := w.Write([]byte(pluginResp.HTML)); err != nil && h.logger != nil {
		h.logger.Warnw("Canvas export not delivered", "canvas_id", canvasID, "bytes", len(pluginResp.HTML), "error", err)
	}
}

// getServerPort returns the server port for internal plugin calls
func (h *CanvasHandler) getServerPort() int {
	// Use configured port if available
	if h.serverPort != 0 {
		return h.serverPort
	}

	// Fallback to env var (useful for testing/dev)
	port := os.Getenv("QNTX_PORT")
	if port != "" {
		// A QNTX_PORT that will not parse is not port 0. Zero means zero.
		portNum, err := strconv.Atoi(port)
		if err != nil {
			if h.logger != nil {
				h.logger.Errorw("QNTX_PORT is not a number", "qntx_port", port, "error", err)
			}
			return 0
		}
		return portNum
	}

	return 0
}

func (h *CanvasHandler) writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *CanvasHandler) writeError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status is already sent, so a failed body leaves the client with a
	// bare status and no reason. This is the last place that still knows it.
	if encErr := json.NewEncoder(w).Encode(map[string]string{
		"error": err.Error(),
	}); encErr != nil && h.logger != nil {
		h.logger.Warnw("Error body not delivered", "status", status, "error", err, "encode_error", encErr)
	}
}

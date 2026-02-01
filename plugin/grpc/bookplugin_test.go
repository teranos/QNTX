// Package grpc provides a test plugin for validating the plugin system.
// This plugin implements a Books and Authors domain with WebSocket support.
package grpc

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap"
)

// BookPlugin is a test plugin featuring classic computer science books.
// The plugin demonstrates a book collector creating attestations about their collection.
type BookPlugin struct {
	mu       sync.RWMutex
	books    map[string]*Book
	authors  map[string]*Author
	logger   *zap.SugaredLogger
	services plugin.ServiceRegistry // QNTX services for creating attestations
}

// Book represents a book in the test plugin.
type Book struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	AuthorIDs []string `json:"author_ids"`
	Year      int      `json:"year,omitempty"`
}

// Author represents an author in the test plugin.
type Author struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// NewBookPlugin creates a new book plugin instance.
func NewBookPlugin() *BookPlugin {
	return &BookPlugin{
		books:   make(map[string]*Book),
		authors: make(map[string]*Author),
	}
}

// Metadata returns the plugin metadata.
func (p *BookPlugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Name:        "book-plugin",
		Version:     "0.1.0",
		QNTXVersion: ">=0.1.0",
		Description: "Test plugin featuring classic computer science books",
		Author:      "QNTX Test Suite",
		License:     "MIT",
	}
}

// Initialize initializes the plugin with QNTX services.
func (p *BookPlugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	p.services = services
	p.logger = services.Logger("book-plugin")
	p.logger.Info("Book plugin initialized")

	// Initialize authors
	p.authors["aristotle"] = &Author{ID: "aristotle", Name: "Aristotle"}
	p.authors["euclid"] = &Author{ID: "euclid", Name: "Euclid"}
	p.authors["isidore"] = &Author{ID: "isidore", Name: "Isidore of Seville"}
	p.authors["bacon"] = &Author{ID: "bacon", Name: "Francis Bacon"}
	p.authors["boyle"] = &Author{ID: "boyle", Name: "Robert Boyle"}
	p.authors["locke"] = &Author{ID: "locke", Name: "John Locke"}
	p.authors["boole"] = &Author{ID: "boole", Name: "George Boole"}
	p.authors["turing"] = &Author{ID: "turing", Name: "Alan Turing"}
	p.authors["shannon"] = &Author{ID: "shannon", Name: "Claude Shannon"}
	p.authors["wiener"] = &Author{ID: "wiener", Name: "Norbert Wiener"}
	p.authors["knuth"] = &Author{ID: "knuth", Name: "Donald Knuth"}
	p.authors["dijkstra"] = &Author{ID: "dijkstra", Name: "Edsger Dijkstra"}
	p.authors["kernighan"] = &Author{ID: "kernighan", Name: "Brian Kernighan"}
	p.authors["ritchie"] = &Author{ID: "ritchie", Name: "Dennis Ritchie"}
	p.authors["mcilroy"] = &Author{ID: "mcilroy", Name: "Doug McIlroy"}
	p.authors["lamport"] = &Author{ID: "lamport", Name: "Leslie Lamport"}
	p.authors["tanenbaum"] = &Author{ID: "tanenbaum", Name: "Andrew Tanenbaum"}
	p.authors["raymond"] = &Author{ID: "raymond", Name: "Eric Raymond"}
	p.authors["abelson"] = &Author{ID: "abelson", Name: "Harold Abelson"}
	p.authors["sussman"] = &Author{ID: "sussman", Name: "Gerald Jay Sussman"}
	p.authors["fowler"] = &Author{ID: "fowler", Name: "Martin Fowler"}
	p.authors["hunt"] = &Author{ID: "hunt", Name: "Andrew Hunt"}
	p.authors["thomas"] = &Author{ID: "thomas", Name: "David Thomas"}
	p.authors["kleppmann"] = &Author{ID: "kleppmann", Name: "Martin Kleppmann"}

	// Initialize books (chronological order)
	p.books["organon"] = &Book{
		ID:        "organon",
		Title:     "Organon",
		AuthorIDs: []string{"aristotle"},
		Year:      -350,
	}
	p.books["elements"] = &Book{
		ID:        "elements",
		Title:     "Elements",
		AuthorIDs: []string{"euclid"},
		Year:      -300,
	}
	p.books["etymologiae"] = &Book{
		ID:        "etymologiae",
		Title:     "Etymologiae",
		AuthorIDs: []string{"isidore"},
		Year:      625,
	}
	p.books["novum-organum"] = &Book{
		ID:        "novum-organum",
		Title:     "Novum Organum",
		AuthorIDs: []string{"bacon"},
		Year:      1620,
	}
	p.books["new-experiments"] = &Book{
		ID:        "new-experiments",
		Title:     "New Experiments Physico Mechanical",
		AuthorIDs: []string{"boyle"},
		Year:      1660,
	}
	p.books["human-understanding"] = &Book{
		ID:        "human-understanding",
		Title:     "An Essay Concerning Human Understanding",
		AuthorIDs: []string{"locke"},
		Year:      1689,
	}
	p.books["laws-of-thought"] = &Book{
		ID:        "laws-of-thought",
		Title:     "An Investigation of the Laws of Thought",
		AuthorIDs: []string{"boole"},
		Year:      1854,
	}
	p.books["computable-numbers"] = &Book{
		ID:        "computable-numbers",
		Title:     "On Computable Numbers",
		AuthorIDs: []string{"turing"},
		Year:      1936,
	}
	p.books["information-theory"] = &Book{
		ID:        "information-theory",
		Title:     "A Mathematical Theory of Communication",
		AuthorIDs: []string{"shannon"},
		Year:      1948,
	}
	p.books["cybernetics"] = &Book{
		ID:        "cybernetics",
		Title:     "Cybernetics",
		AuthorIDs: []string{"wiener"},
		Year:      1948,
	}
	p.books["taocp"] = &Book{
		ID:        "taocp",
		Title:     "The Art of Computer Programming",
		AuthorIDs: []string{"knuth"},
		Year:      1968,
	}
	p.books["discipline-programming"] = &Book{
		ID:        "discipline-programming",
		Title:     "A Discipline of Programming",
		AuthorIDs: []string{"dijkstra"},
		Year:      1976,
	}
	p.books["c-programming"] = &Book{
		ID:        "c-programming",
		Title:     "The C Programming Language",
		AuthorIDs: []string{"kernighan", "ritchie"},
		Year:      1978,
	}
	p.books["time-clocks-ordering"] = &Book{
		ID:        "time-clocks-ordering",
		Title:     "Time, Clocks, and the Ordering of Events",
		AuthorIDs: []string{"lamport"},
		Year:      1978,
	}
	p.books["unix-philosophy"] = &Book{
		ID:        "unix-philosophy",
		Title:     "Unix Philosophy",
		AuthorIDs: []string{"mcilroy"},
		Year:      1978,
	}
	p.books["os-design"] = &Book{
		ID:        "os-design",
		Title:     "Operating Systems: Design and Implementation",
		AuthorIDs: []string{"tanenbaum"},
		Year:      1987,
	}
	p.books["cathedral-bazaar"] = &Book{
		ID:        "cathedral-bazaar",
		Title:     "The Cathedral and the Bazaar",
		AuthorIDs: []string{"raymond"},
		Year:      1999,
	}
	p.books["sicp"] = &Book{
		ID:        "sicp",
		Title:     "Structure and Interpretation of Computer Programs",
		AuthorIDs: []string{"abelson", "sussman"},
		Year:      1985,
	}
	p.books["refactoring"] = &Book{
		ID:        "refactoring",
		Title:     "Refactoring",
		AuthorIDs: []string{"fowler"},
		Year:      1999,
	}
	p.books["pragmatic-programmer"] = &Book{
		ID:        "pragmatic-programmer",
		Title:     "The Pragmatic Programmer",
		AuthorIDs: []string{"hunt", "thomas"},
		Year:      1999,
	}
	p.books["ddia"] = &Book{
		ID:        "ddia",
		Title:     "Designing Data Intensive Applications",
		AuthorIDs: []string{"kleppmann"},
		Year:      2017,
	}

	// Create attestations for book collector tracking auction availability (Issue #138 demo)
	// This demonstrates gRPC plugins using ATSStore and Queue services via gRPC
	if err := p.setupBookCollector(ctx); err != nil {
		p.logger.Warnw("Failed to setup book collector attestations", "error", err)
		// Continue anyway - attestations are optional for plugin functionality
	}

	return nil
}

// setupBookCollector creates attestations and monitoring jobs for a rare book collector.
// The collector tracks books they want, auction houses announce availability,
// and the plugin queries for matches and monitors for new opportunities.
func (p *BookPlugin) setupBookCollector(ctx context.Context) error {
	// Get ATSStore - works for all plugins via unified interface
	store := p.services.ATSStore()
	if store == nil {
		p.logger.Debug("ATSStore not available, skipping attestation creation")
		return nil
	}

	queue := p.services.Queue()
	const collectorID = "collector"

	// Step 1: Collector expresses interest in rare books across all eras
	wantedBooks := []string{
		// Ancient works
		"organon", "elements",
		// Medieval
		"etymologiae",
		// Early modern science
		"novum-organum", "new-experiments", "human-understanding",
		// 19th century
		"laws-of-thought",
		// 20th century computing foundations
		"computable-numbers", "information-theory", "cybernetics",
		// Classic CS texts
		"time-clocks-ordering", "sicp", "taocp",
	}
	for _, bookID := range wantedBooks {
		cmd := &types.AsCommand{
			Subjects:   []string{collectorID},
			Predicates: []string{"wants"},
			Contexts:   []string{bookID},
		}
		if _, err := store.GenerateAndCreateAttestation(context.Background(), cmd); err != nil {
			return err
		}
	}
	p.logger.Infow("Collector expressed interest in books", "count", len(wantedBooks))

	// Step 2: Auction houses/sellers announce availability
	// (Simulating marketplace activity)
	availableBooks := map[string]string{
		"organon":              "christie-auctions",
		"elements":             "sothebys",
		"computable-numbers":   "heritage-auctions",
		"information-theory":   "rare-books-dealer",
		"time-clocks-ordering": "acm-library-sale",
	}
	for bookID, seller := range availableBooks {
		cmd := &types.AsCommand{
			Subjects:   []string{seller},
			Predicates: []string{"offers"},
			Contexts:   []string{bookID},
		}
		if _, err := store.GenerateAndCreateAttestation(context.Background(), cmd); err != nil {
			return err
		}
	}
	p.logger.Infow("Auction houses announced availability", "count", len(availableBooks))

	// Step 3: Query for matches (books the collector wants that are available)
	matches, err := p.findMatches(ctx, collectorID, wantedBooks)
	if err != nil {
		return err
	}
	p.logger.Infow("Found matching books available for auction",
		"matches", matches,
		"count", len(matches),
	)

	// Step 4: Enqueue background monitoring job via Queue service
	if queue != nil {
		// In a real plugin, this would be a recurring job
		// For demo purposes, we just show the Queue service integration
		p.logger.Info("Queue service available - could enqueue monitoring job for new availability")
		// TODO: Enqueue job when Queue service gRPC client is implemented
	} else {
		p.logger.Debug("Queue service not available")
	}

	return nil
}

// findMatches queries ATSStore to find books the collector wants that are available.
func (p *BookPlugin) findMatches(ctx context.Context, collectorID string, wantedBooks []string) ([]string, error) {
	store := p.services.ATSStore()
	if store == nil {
		return nil, nil
	}

	var matches []string

	// Query all attestations to find matches
	// In production, we'd use more specific filters, but current implementation
	// only supports Actor filter, so we query all and filter in-memory
	filter := ats.AttestationFilter{
		Limit: 1000,
	}
	attestations, err := store.GetAttestations(context.Background(), filter)
	if err != nil {
		return nil, err
	}

	// Build index of wants and offers
	wants := make(map[string]bool)
	offers := make(map[string]string) // bookID -> seller

	for _, att := range attestations {
		if len(att.Subjects) == 0 || len(att.Predicates) == 0 || len(att.Contexts) == 0 {
			continue
		}

		subject := att.Subjects[0]
		predicate := att.Predicates[0]
		context := att.Contexts[0]

		if subject == collectorID && predicate == "wants" {
			wants[context] = true
		}

		if predicate == "offers" {
			offers[context] = subject
		}
	}

	// Find books that are both wanted and offered
	for bookID := range wants {
		if seller, offered := offers[bookID]; offered {
			matches = append(matches, bookID)
			p.logger.Infow("Match found",
				"book", bookID,
				"seller", seller,
			)
		}
	}

	return matches, nil
}

// Shutdown gracefully shuts down the plugin.
func (p *BookPlugin) Shutdown(ctx context.Context) error {
	p.logger.Info("Book plugin shutting down")
	return nil
}

// RegisterHTTP registers HTTP handlers for the books domain.
func (p *BookPlugin) RegisterHTTP(mux *http.ServeMux) error {
	// GET /api/book-plugin/books - List all books
	mux.HandleFunc("GET /api/book-plugin/books", p.handleListBooks)

	// GET /api/book-plugin/authors - List all authors
	mux.HandleFunc("GET /api/book-plugin/authors", p.handleListAuthors)

	return nil
}

// handleListBooks returns all books as JSON.
func (p *BookPlugin) handleListBooks(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	books := make([]*Book, 0, len(p.books))
	for _, book := range p.books {
		books = append(books, book)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(books)
}

// handleListAuthors returns all authors as JSON.
func (p *BookPlugin) handleListAuthors(w http.ResponseWriter, r *http.Request) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	authors := make([]*Author, 0, len(p.authors))
	for _, author := range p.authors {
		authors = append(authors, author)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(authors)
}

// RegisterWebSocket registers WebSocket handlers for the books domain.
func (p *BookPlugin) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	handlers := make(map[string]plugin.WebSocketHandler)
	handlers["/book-plugin-ws"] = &echoWebSocketHandler{logger: p.logger}
	return handlers, nil
}

// Health returns the health status of the plugin.
func (p *BookPlugin) Health(ctx context.Context) plugin.HealthStatus {
	return plugin.HealthStatus{
		Healthy: true,
		Message: "Book plugin is healthy",
		Details: map[string]interface{}{
			"books":   len(p.books),
			"authors": len(p.authors),
		},
	}
}

// echoWebSocketHandler implements a simple WebSocket echo server for testing.
type echoWebSocketHandler struct {
	logger *zap.SugaredLogger
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for testing
	},
}

// ServeWS handles WebSocket connections and echoes back messages.
func (h *echoWebSocketHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Errorw("WebSocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	h.logger.Info("WebSocket connection established")

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.logger.Errorw("WebSocket read error", "error", err)
			}
			break
		}

		h.logger.Infow("Received WebSocket message", "type", messageType, "size", len(data))

		// Echo the message back
		err = conn.WriteMessage(messageType, data)
		if err != nil {
			h.logger.Errorw("WebSocket write error", "error", err)
			break
		}
	}

	h.logger.Info("WebSocket connection closed")
}

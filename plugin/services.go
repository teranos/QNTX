package plugin

import (
	"context"
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

// QueueService defines the job queue operations available to plugins.
// This interface allows both local and remote queue implementations.
type QueueService interface {
	// Enqueue adds a new job to the queue
	Enqueue(job *async.Job) error

	// GetJob retrieves a job by ID
	GetJob(id string) (*async.Job, error)

	// UpdateJob updates a job's state
	UpdateJob(job *async.Job) error

	// ListJobs lists jobs with optional status filter
	ListJobs(status *async.JobStatus, limit int) ([]*async.Job, error)
}

// ScheduleService defines runtime schedule management for plugins.
// Plugins use this to create, pause, resume, and delete recurring Pulse schedules.
type ScheduleService interface {
	// Create creates a new recurring schedule and returns its ID
	Create(handlerName string, intervalSecs int, payload []byte, metadata map[string]string) (scheduleID string, err error)

	// Pause pauses an active schedule
	Pause(scheduleID string) error

	// Resume resumes a paused schedule
	Resume(scheduleID string) error

	// Delete soft-deletes a schedule
	Delete(scheduleID string) error

	// Get retrieves a schedule by ID
	Get(scheduleID string) (*schedule.Job, error)
}

// FileService provides file access for plugins.
// Plugins use this to read files stored on the core server's filesystem.
type FileService interface {
	// ReadFileBase64 reads a stored file and returns its MIME type and base64-encoded content.
	ReadFileBase64(fileID string) (mimeType, base64Data string, err error)
}

// LLMService provides provider-agnostic LLM access for plugins.
// Core routes requests to the appropriate provider plugin.
type LLMService interface {
	// Chat sends a chat completion request and returns the response.
	Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error)
}

// LLMRequest is a provider-agnostic LLM chat request.
type LLMRequest struct {
	SystemPrompt string
	UserPrompt   string
	Model        string
	Temperature  float64
	MaxTokens    int
	Provider     string          // Target provider (empty = default)
	Attachments  []LLMAttachment // Multimodal attachments
}

// LLMAttachment is a file attached to an LLM request.
type LLMAttachment struct {
	MimeType string
	Data     string
	Filename string
}

// LLMResponse is a provider-agnostic LLM chat response.
type LLMResponse struct {
	Content          string
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// VectorSearchService provides nearest-neighbor search over dense vector indexes (ADR-016).
// Core routes requests to the provider plugin (e.g. faiss).
type VectorSearchService interface {
	// Search finds the nearest neighbors to a query vector in a named index.
	Search(ctx context.Context, req VectorSearchRequest) (*VectorSearchResponse, error)

	// AddVectors inserts vectors into a named index.
	AddVectors(ctx context.Context, req AddVectorsRequest) (*AddVectorsResponse, error)

	// CreateIndex creates a new named vector index.
	CreateIndex(ctx context.Context, req CreateIndexRequest) (*CreateIndexResponse, error)
}

// VectorSearchRequest is a request to search a vector index.
type VectorSearchRequest struct {
	Index       string
	QueryVector []float32
	TopK        int
}

// VectorSearchResponse contains search results.
type VectorSearchResponse struct {
	Results []VectorSearchHit
}

// VectorSearchHit is a single search result.
type VectorSearchHit struct {
	ID       string
	Distance float32
}

// AddVectorsRequest is a request to add vectors to an index.
type AddVectorsRequest struct {
	Index   string
	Vectors []VectorEntry
}

// VectorEntry is a single vector with its identifier.
type VectorEntry struct {
	ID     string
	Vector []float32
}

// AddVectorsResponse is the result of adding vectors.
type AddVectorsResponse struct {
	Added int
}

// CreateIndexRequest is a request to create a new vector index.
type CreateIndexRequest struct {
	Name       string
	Dimensions int
}

// CreateIndexResponse is the result of creating an index.
type CreateIndexResponse struct {
	Name string
}

// SearchService provides full-text search over indexed documents.
// Core routes requests to the search provider plugin (qntx-meili).
type SearchService interface {
	// Search queries an index and returns ranked results.
	Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)

	// IndexDocuments pushes documents into an index.
	IndexDocuments(ctx context.Context, req IndexDocumentsRequest) (*IndexDocumentsResponse, error)

	// DeleteDocuments removes documents from an index by ID.
	DeleteDocuments(ctx context.Context, req DeleteDocumentsRequest) (*DeleteDocumentsResponse, error)
}

// SearchRequest is a search query against an index.
type SearchRequest struct {
	Query   string
	Index   string
	TopK    int
	Filters []byte // filter expression as JSON — interpreted by the provider
}

// SearchResponse contains ranked search results.
type SearchResponse struct {
	Hits         []SearchHit
	Total        int
	ProcessingMs int
}

// SearchHit is a single search result.
type SearchHit struct {
	ID       string
	Score    float32
	Document []byte // indexed content as JSON
}

// IndexDocumentsRequest pushes documents into an index.
type IndexDocumentsRequest struct {
	Index     string
	Documents [][]byte // documents as JSON
}

// IndexDocumentsResponse reports how many documents were accepted.
type IndexDocumentsResponse struct {
	Accepted int
}

// DeleteDocumentsRequest removes documents from an index.
type DeleteDocumentsRequest struct {
	Index string
	IDs   []string
}

// DeleteDocumentsResponse reports how many documents were deleted.
type DeleteDocumentsResponse struct {
	Deleted int
}

// ServiceRegistry provides access to QNTX core services for domain plugins.
// Plugins use this registry to look up services they need.
type ServiceRegistry interface {
	// Database returns the shared QNTX database connection
	Database() *sql.DB

	// Logger returns a logger for this plugin
	Logger(domain string) *zap.SugaredLogger

	// Config returns plugin-specific configuration
	Config(domain string) Config

	// ATSStore returns the attestation storage interface
	ATSStore() ats.AttestationStore

	// Queue returns the Pulse async job queue
	Queue() QueueService

	// Schedule returns the Pulse schedule management service
	Schedule() ScheduleService

	// FileService returns the file storage service for reading uploaded files
	FileService() FileService

	// LLM returns the LLM service for provider-agnostic chat completions.
	// Returns nil if no LLM provider is available.
	LLM() LLMService

	// VectorSearch returns the vector search service for nearest-neighbor queries (ADR-016).
	// Returns nil if no vector search provider is available.
	VectorSearch() VectorSearchService

	// Search returns the full-text search service.
	// Returns nil if no search provider is available.
	Search() SearchService
}

// Config provides access to plugin configuration
type Config interface {
	// GetString retrieves a string configuration value
	GetString(key string) string

	// GetInt retrieves an integer configuration value
	GetInt(key string) int

	// GetBool retrieves a boolean configuration value
	GetBool(key string) bool

	// GetStringSlice retrieves a string slice configuration value
	GetStringSlice(key string) []string

	// Get retrieves a raw configuration value
	Get(key string) interface{}

	// Set sets a configuration value (for runtime overrides)
	Set(key string, value interface{})

	// GetKeys returns all available configuration keys (sorted)
	GetKeys() []string
}

// DefaultServiceRegistry is the standard implementation of ServiceRegistry
type DefaultServiceRegistry struct {
	db       *sql.DB
	logger   *zap.SugaredLogger
	store    ats.AttestationStore
	config   ConfigProvider
	queue    QueueService
	registry *Registry // Reference to plugin registry for metadata lookup
}

// ConfigProvider provides configuration for plugins
type ConfigProvider interface {
	// GetPluginConfig returns configuration for a specific plugin
	GetPluginConfig(domain string) Config
}

// NewServiceRegistry creates a new service registry
func NewServiceRegistry(db *sql.DB, logger *zap.SugaredLogger, store ats.AttestationStore, config ConfigProvider, queue QueueService) ServiceRegistry {
	return &DefaultServiceRegistry{
		db:       db,
		logger:   logger,
		store:    store,
		config:   config,
		queue:    queue,
		registry: GetDefaultRegistry(), // Access global registry for plugin metadata
	}
}

// Database returns the shared QNTX database connection
func (r *DefaultServiceRegistry) Database() *sql.DB {
	return r.db
}

// Logger returns a logger for the specified domain with version information
func (r *DefaultServiceRegistry) Logger(domain string) *zap.SugaredLogger {
	// Look up plugin metadata to include version in logger name
	loggerName := domain
	if r.registry != nil {
		if plugin, ok := r.registry.Get(domain); ok {
			metadata := plugin.Metadata()
			if metadata.Version != "" {
				// Format as: domain v0.4.3 (version in separate field for coloring)
				loggerName = domain + " v" + metadata.Version
			}
		}
	}
	return r.logger.Named(loggerName)
}

// Config returns plugin-specific configuration
func (r *DefaultServiceRegistry) Config(domain string) Config {
	return r.config.GetPluginConfig(domain)
}

// ATSStore returns the attestation storage interface
func (r *DefaultServiceRegistry) ATSStore() ats.AttestationStore {
	return r.store
}

// Queue returns the Pulse async job queue
func (r *DefaultServiceRegistry) Queue() QueueService {
	return r.queue
}

// Schedule returns nil for in-process plugins (runtime schedules are a gRPC feature).
func (r *DefaultServiceRegistry) Schedule() ScheduleService {
	return nil
}

// FileService returns nil for in-process plugins (they share the filesystem with core).
func (r *DefaultServiceRegistry) FileService() FileService {
	return nil
}

// LLM returns nil for in-process plugins (LLM is a gRPC-only service).
func (r *DefaultServiceRegistry) LLM() LLMService {
	return nil
}

// VectorSearch returns nil for in-process plugins (VectorSearch is a gRPC-only service).
func (r *DefaultServiceRegistry) VectorSearch() VectorSearchService {
	return nil
}

// Search returns nil for in-process plugins (Search is a gRPC-only service).
func (r *DefaultServiceRegistry) Search() SearchService {
	return nil
}

package grpc

import (
	"context"
	"crypto/rand"
	"net"
	"sync"
	"sync/atomic"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/sacred"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// ServiceEndpoints holds the addresses of running service servers
type ServiceEndpoints struct {
	ATSStoreAddress     string
	QueueAddress        string
	ScheduleAddress     string
	FileServiceAddress  string
	LLMAddress          string
	EmbeddingAddress    string
	VectorSearchAddress string
	GroundAddress       string
	SearchAddress       string
	FetchAddress        string
	MailAddress         string
	AuthToken           string
}

// ServicesManager manages gRPC service servers for plugin callbacks
type ServicesManager struct {
	atsStoreServer     *grpc.Server
	atsStore           *services.ATSStoreServer // for stream cancellation on plugin restart
	queueServer        *grpc.Server
	scheduleServer     *grpc.Server
	scheduleSrv        *services.ScheduleServer // handed the callers of open sigil calls
	openRun            atomic.Pointer[OpenRun]
	fileServiceServer  *grpc.Server
	llmServer          *grpc.Server
	llm                routing[*services.LLMServer]
	llmConfig          config.LLMConfig
	embeddingServer    *grpc.Server
	embeddingRouter    *services.EmbeddingServer // Exposed for late backend registration
	vectorSearchServer *grpc.Server
	vectorSearchRouter *services.VectorSearchServer // Exposed for provider registration after plugin init
	groundServer       *grpc.Server
	groundDBPath       string
	searchServer       *grpc.Server
	search             routing[*services.SearchServer]
	fetchServer        *grpc.Server
	fetchSrv           *services.FetchServer // for version resolver injection
	fetchCfg           config.FetchConfig
	node               string // The node's DID, who writes what a plugin named no actor for
	mailServer         *grpc.Server
	mailSrv            *services.MailServer // wired once the node has Users and its DID
	endpoints          ServiceEndpoints
	logger             *zap.SugaredLogger
	// running is every service server that started, in the order it did.
	running []*grpc.Server

	// Services that failed to start, by name. QNTX boots without them, so the
	// reason has to outlive the boot — a plugin failing to reach one later is
	// the moment this needs to be readable.
	degradedMu sync.Mutex
	degraded   map[string]string
}

// Degraded returns the services that failed to start and why.
func (m *ServicesManager) Degraded() map[string]string {
	m.degradedMu.Lock()
	defer m.degradedMu.Unlock()
	out := make(map[string]string, len(m.degraded))
	for name, reason := range m.degraded {
		out[name] = reason
	}
	return out
}

// noteDegraded records a service QNTX will run without, loudly.
func (m *ServicesManager) noteDegraded(service string, err error) {
	m.logger.Errorw("Service did not start; plugins will not have it",
		"service", service, "error", err)
	m.degradedMu.Lock()
	defer m.degradedMu.Unlock()
	m.degraded[service] = err.Error()
}

// routing is a router providers register with once their plugin is up, or why
// the node runs without it.
type routing[R any] struct {
	router R
	why    error
}

// NewServicesManager creates a new services manager. node is the node's DID.
// Until the node hands over how to mint a run's store token, minting one is
// refused.
func NewServicesManager(llmCfg config.LLMConfig, fetchCfg config.FetchConfig, node string, logger *zap.SugaredLogger) *ServicesManager {
	m := &ServicesManager{
		llmConfig: llmCfg,
		fetchCfg:  fetchCfg,
		node:      node,
		logger:    logger,
		degraded:  map[string]string{},
		llm:       routing[*services.LLMServer]{why: errors.New("the LLM service has not started")},
		search:    routing[*services.SearchServer]{why: errors.New("the search service has not started")},
	}
	notYet := OpenRun(func(_, namespace string) (string, func(), error) {
		return "", nil, errors.Newf("the node has not handed over how to mint a store token for namespace %s yet", namespace)
	})
	m.openRun.Store(&notYet)
	return m
}

// Start starts the gRPC service servers with dynamic port allocation.
// filesDir is the path to stored files (for the FileService).
// groundDBPath is the path to Ground's SQLite database; with none, the Ground
// service refuses each write, saying ground_db_path is not configured.
//
// The services the node hands things to after Start (the ATS store, schedule,
// fetch and mail services) are made before any listens, so each is there
// whether or not its server came to serve.
func (m *ServicesManager) Start(ctx context.Context, store ats.AttestationStore, queue *async.Queue, scheduleStore *schedule.Store, filesDir string, groundDBPath string) (*ServiceEndpoints, error) {
	m.groundDBPath = groundDBPath
	// The token plugins reach every service with
	authToken := rand.Text()
	m.atsStore = services.NewATSStoreServer(store, authToken, m.node, m.logger)
	m.scheduleSrv = services.NewScheduleServer(scheduleStore, authToken, m.logger)
	m.fetchSrv = services.NewFetchServer(store, authToken, m.node, m.fetchCfg, m.logger)
	m.mailSrv = services.NewMailServer(authToken, m.logger)

	// Start ATSStore service
	atsStoreAddr, err := m.startATSStoreService(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to start ATS store service")
	}

	// Start Queue service
	queueAddr, err := m.startQueueService(ctx, queue, authToken)
	if err != nil {
		m.stopRunning()
		return nil, errors.Wrap(err, "failed to start queue service")
	}

	// Start Schedule service
	scheduleAddr, err := m.startScheduleService(ctx)
	if err != nil {
		m.stopRunning()
		return nil, errors.Wrap(err, "failed to start schedule service")
	}

	// Start File service
	fileServiceAddr, err := m.startFileService(ctx, filesDir, authToken)
	if err != nil {
		m.stopRunning()
		return nil, errors.Wrap(err, "failed to start file service")
	}

	// Start LLM service (starts empty, providers register after plugin init)
	llmAddr, err := m.startLLMService(ctx, store)
	if err != nil {
		m.noteDegraded("llm", err)
		m.llm.why = errors.Wrap(err, "the LLM service did not start")
		llmAddr = ""
	}

	// Start Embedding service (starts empty, backend registers after embedding engine init)
	embeddingAddr, err := m.startEmbeddingService(ctx, authToken)
	if err != nil {
		m.noteDegraded("embedding", err)
		embeddingAddr = ""
	}

	// Start VectorSearch service (starts empty, provider registers after plugin init)
	vectorSearchAddr, err := m.startVectorSearchService(ctx, authToken)
	if err != nil {
		m.noteDegraded("vectorsearch", err)
		vectorSearchAddr = ""
	}

	// Start Ground service
	groundAddr, err := m.startGroundService(ctx, groundDBPath, authToken)
	if err != nil {
		m.noteDegraded("ground", err)
		groundAddr = ""
	}

	// Start Search service (starts empty, provider registers after plugin init)
	searchAddr, err := m.startSearchService(ctx)
	if err != nil {
		m.noteDegraded("search", err)
		m.search.why = errors.Wrap(err, "the search service did not start")
		searchAddr = ""
	}

	// Start Fetch service
	fetchAddr, err := m.startFetchService(ctx)
	if err != nil {
		m.noteDegraded("fetch", err)
		fetchAddr = ""
	}

	// Start Mail service (sends nothing until the node wires it: ADR-041)
	mailAddr, err := m.startMailService(ctx)
	if err != nil {
		m.noteDegraded("mail", err)
		mailAddr = ""
	}

	m.endpoints = ServiceEndpoints{
		ATSStoreAddress:     atsStoreAddr,
		QueueAddress:        queueAddr,
		ScheduleAddress:     scheduleAddr,
		FileServiceAddress:  fileServiceAddr,
		LLMAddress:          llmAddr,
		EmbeddingAddress:    embeddingAddr,
		VectorSearchAddress: vectorSearchAddr,
		GroundAddress:       groundAddr,
		SearchAddress:       searchAddr,
		FetchAddress:        fetchAddr,
		MailAddress:         mailAddr,
		AuthToken:           authToken,
	}

	return &m.endpoints, nil
}

// serve runs one gRPC service: it answers on the listener until that closes,
// and stops gracefully when ctx is done.
//
// Both goroutines go through sacred. These are where a plugin's own code is
// reached, so a panic in a handler was the end of the node — which made every
// plugin a way to stop everything else running on it. Ten services had a
// hand-written copy of this pair, twenty of the eighty-eight goroutines the
// node could die inside without saying anything.
func (m *ServicesManager) serve(
	ctx context.Context, name string, server *grpc.Server, listener net.Listener,
) {
	m.running = append(m.running, server)
	sacred.Go("grpc."+name+".shutdown", func() {
		<-ctx.Done()
		m.logger.Debugw("Context cancelled, stopping service", "service", name)
		server.GracefulStop()
	})
	sacred.Go("grpc."+name+".serve", func() {
		// A service that stops serving is one plugins run without from then on.
		if err := server.Serve(listener); err != nil {
			m.noteDegraded(name, errors.Wrapf(err, "gRPC service %s stopped serving at %s", name, listener.Addr()))
		}
	})
}

// startATSStoreService starts the ATSStore gRPC service
func (m *ServicesManager) startATSStoreService(ctx context.Context) (string, error) {
	// Listen on dynamic port
	// Use explicit IPv4 127.0.0.1 instead of "localhost" to avoid IPv6 [::1] resolution
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	// Create gRPC server
	m.atsStoreServer = grpc.NewServer()
	protocol.RegisterATSStoreServiceServer(m.atsStoreServer, m.atsStore)

	m.serve(ctx, "ATSStore", m.atsStoreServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("ATSStore service started", "address", addr)

	return addr, nil
}

// startQueueService starts the Queue gRPC service
func (m *ServicesManager) startQueueService(ctx context.Context, queue *async.Queue, authToken string) (string, error) {
	// Listen on dynamic port
	// Use explicit IPv4 127.0.0.1 instead of "localhost" to avoid IPv6 [::1] resolution
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	// Create gRPC server
	m.queueServer = grpc.NewServer()
	queueServer := services.NewQueueServer(queue, authToken, m.logger)
	protocol.RegisterQueueServiceServer(m.queueServer, queueServer)

	m.serve(ctx, "Queue", m.queueServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("Queue service started", "address", addr)

	return addr, nil
}

// startScheduleService starts the Schedule gRPC service
func (m *ServicesManager) startScheduleService(ctx context.Context) (string, error) {
	// Listen on dynamic port
	// Use explicit IPv4 127.0.0.1 instead of "localhost" to avoid IPv6 [::1] resolution
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	// Create gRPC server
	m.scheduleServer = grpc.NewServer()
	protocol.RegisterScheduleServiceServer(m.scheduleServer, m.scheduleSrv)

	m.serve(ctx, "Schedule", m.scheduleServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("Schedule service started", "address", addr)

	return addr, nil
}

// startFileService starts the File gRPC service
func (m *ServicesManager) startFileService(ctx context.Context, filesDir string, authToken string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.fileServiceServer = grpc.NewServer()
	fileServer := services.NewFileServiceServer(filesDir, authToken, m.logger)
	protocol.RegisterFileServiceServer(m.fileServiceServer, fileServer)

	m.serve(ctx, "File", m.fileServiceServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("File service started", "address", addr)

	return addr, nil
}

// startLLMService starts the LLM routing gRPC service.
// The server starts empty — providers register after their own initialization completes.
func (m *ServicesManager) startLLMService(ctx context.Context, store ats.AttestationStore) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	router := services.NewLLMServer(m.llmConfig, store, m.logger)
	m.llm = routing[*services.LLMServer]{router: router}
	m.llmServer = grpc.NewServer()
	protocol.RegisterLLMServiceServer(m.llmServer, router)

	m.serve(ctx, "LLM", m.llmServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("LLM service started", "address", addr)

	return addr, nil
}

// startEmbeddingService starts the Embedding gRPC service.
// The server starts empty — the backend registers after the embedding engine initializes.
func (m *ServicesManager) startEmbeddingService(ctx context.Context, authToken string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.embeddingRouter = services.NewEmbeddingServer(authToken, m.logger)
	m.embeddingServer = grpc.NewServer()
	protocol.RegisterEmbeddingServiceServer(m.embeddingServer, m.embeddingRouter)

	m.serve(ctx, "Embedding", m.embeddingServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("Embedding service started", "address", addr)

	return addr, nil
}

// startVectorSearchService starts the VectorSearch routing gRPC service.
// The server starts empty — the provider registers after its own initialization completes.
func (m *ServicesManager) startVectorSearchService(ctx context.Context, authToken string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.vectorSearchRouter = services.NewVectorSearchServer(authToken, m.logger)
	m.vectorSearchServer = grpc.NewServer()
	protocol.RegisterVectorSearchServiceServer(m.vectorSearchServer, m.vectorSearchRouter)

	m.serve(ctx, "VectorSearch", m.vectorSearchServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("VectorSearch service started", "address", addr)

	return addr, nil
}

// startGroundService starts the Ground gRPC service
func (m *ServicesManager) startGroundService(ctx context.Context, dbPath string, authToken string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.groundServer = grpc.NewServer()
	groundServer := services.NewGroundServer(dbPath, authToken, m.logger)
	protocol.RegisterGroundServiceServer(m.groundServer, groundServer)

	m.serve(ctx, "Ground", m.groundServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("Ground service started", "address", addr)

	return addr, nil
}

// startSearchService starts the Search routing gRPC service.
// The server starts empty — the provider registers after its own initialization completes.
func (m *ServicesManager) startSearchService(ctx context.Context) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	router := services.NewSearchServer(m.logger)
	m.search = routing[*services.SearchServer]{router: router}
	m.searchServer = grpc.NewServer()
	protocol.RegisterSearchServiceServer(m.searchServer, router)

	m.serve(ctx, "Search", m.searchServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("Search service started", "address", addr)

	return addr, nil
}

func (m *ServicesManager) startFetchService(ctx context.Context) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.fetchServer = grpc.NewServer()
	protocol.RegisterFetchServiceServer(m.fetchServer, m.fetchSrv)

	m.serve(ctx, "Fetch", m.fetchServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("Fetch service started", "address", addr)

	return addr, nil
}

func (m *ServicesManager) startMailService(ctx context.Context) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", errors.Wrap(err, "failed to listen")
	}

	m.mailServer = grpc.NewServer()
	protocol.RegisterMailServiceServer(m.mailServer, m.mailSrv)

	m.serve(ctx, "Mail", m.mailServer, listener)

	addr := listener.Addr().String()
	m.logger.Debugw("Mail service started", "address", addr)

	return addr, nil
}

// MailServer is the mail service, for what the node mails in its own name
// (ADR-042). Made by Start whether or not its server came to serve plugins.
func (m *ServicesManager) MailServer() *services.MailServer {
	return m.mailSrv
}

// SetMail hands the mail service what it sends with. Until then a plugin
// asking to send is told the node has not finished starting.
func (m *ServicesManager) SetMail(w services.MailWiring) {
	m.mailSrv.Wire(w)
}

// SetVersionResolver installs a source→version resolver on ATSStore and Fetch services.
// Call after plugin registry is populated so attestations carry source_version automatically.
func (m *ServicesManager) SetVersionResolver(resolver services.VersionResolver) {
	m.atsStore.SetVersionResolver(resolver)
	m.fetchSrv.SetVersionResolver(resolver)
	m.mailSrv.SetVersionResolver(resolver)
}

// SetCallStores hands the ATS store service the stores of the calls plugins are
// answering, so a plugin reads and writes where its caller acts.
func (m *ServicesManager) SetCallStores(calls services.CallStores) {
	m.atsStore.SetCallStores(calls)
}

// SetPluginStores hands the ATS store and fetch services the stores of the
// namespaces plugins stand in, by the tokens the node handed them (ADR-046).
func (m *ServicesManager) SetPluginStores(plugins services.PluginStores) {
	m.atsStore.SetPluginStores(plugins)
	m.fetchSrv.SetPluginStores(plugins)
}

// SetCallers hands the schedule service the callers of the calls plugins are
// answering, so a schedule created during one remembers who created it and
// where.
func (m *ServicesManager) SetCallers(callers services.Callers) {
	m.scheduleSrv.SetCallers(callers)
}

// OpenRun mints a store token for one run of a job, reaching the store of the
// namespace the schedule's creator acted in, and what closes it once the run
// is done.
type OpenRun func(userID, namespace string) (token string, done func(), err error)

// SetOpenRun hands the manager how the node mints a run's store token.
func (m *ServicesManager) SetOpenRun(open OpenRun) {
	m.openRun.Store(&open)
}

// OpenRunFor mints a run's store token, or says why none can be.
func (m *ServicesManager) OpenRunFor(userID, namespace string) (string, func(), error) {
	if m == nil {
		return "", nil, errors.Newf("no services manager to mint a store token for namespace %s", namespace)
	}
	return (*m.openRun.Load())(userID, namespace)
}

// GetSearchRouter returns the search router for provider registration, or
// why the node runs without one.
func (m *ServicesManager) GetSearchRouter() (*services.SearchServer, error) {
	return m.search.router, m.search.why
}

// CancelATSStreams cancels all active ATSStore streams.
// Called during plugin restart to free the database mutex before launching the new process.
func (m *ServicesManager) CancelATSStreams() {
	m.atsStore.CancelStreams()
}

// GetLLMRouter returns the LLM router for provider registration, or why the
// node runs without one.
func (m *ServicesManager) GetLLMRouter() (*services.LLMServer, error) {
	return m.llm.router, m.llm.why
}

// GetEmbeddingRouter returns the embedding router for backend registration.
// Returns nil if the embedding service is not running.
func (m *ServicesManager) GetEmbeddingRouter() *services.EmbeddingServer {
	return m.embeddingRouter
}

// GetVectorSearchRouter returns the vector search router for provider registration.
// Returns nil if the vector search service is not running.
func (m *ServicesManager) GetVectorSearchRouter() *services.VectorSearchServer {
	return m.vectorSearchRouter
}

// Shutdown gracefully stops all service servers
func (m *ServicesManager) Shutdown() {
	m.logger.Debug("Shutting down plugin services")
	for _, server := range m.running {
		server.GracefulStop()
	}
	m.logger.Debug("Plugin services stopped")
}

// stopRunning stops every service that started, for a Start that cannot finish.
func (m *ServicesManager) stopRunning() {
	for _, server := range m.running {
		server.Stop()
	}
}

// GetEndpoints returns the service endpoints
func (m *ServicesManager) GetEndpoints() *ServiceEndpoints {
	return &m.endpoints
}

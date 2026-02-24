package qntxgithub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// Plugin is the GitHub domain plugin implementation.
type Plugin struct {
	plugin.PauseController
	services plugin.ServiceRegistry

	client *GitHubClient // GitHub API client
}

// NewPlugin creates a new GitHub domain plugin.
func NewPlugin() *Plugin {
	return &Plugin{}
}

// Metadata returns information about the GitHub domain plugin.
func (p *Plugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Name:        "github",
		Version:     "0.1.5",
		QNTXVersion: ">= 0.1.0",
		Description: "GitHub integration for repository events and automation",
		Author:      "QNTX Team",
		License:     "MIT",
	}
}

// Initialize initializes the GitHub domain plugin.
func (p *Plugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	p.services = services
	p.InitPauseController("github")
	logger := services.Logger("github")

	config := services.Config("github")
	token := config.GetString("token")
	if token == "" {
		logger.Warn("No GitHub token configured - API rate limits will be restricted")
	}

	// Create GitHub client
	p.client = NewGitHubClient(token, logger)

	// Get configured repositories to watch
	repos := config.GetStringSlice("repos")
	if len(repos) == 0 {
		logger.Warn("No repositories configured - plugin will not poll for events")
	}

	// Get poll interval (default 5 minutes)
	pollInterval := config.GetInt("poll_interval")
	if pollInterval <= 0 {
		pollInterval = 300 // 5 minutes default
	}

	logger.Infow("GitHub plugin initialized",
		"repos", repos,
		"poll_interval", pollInterval,
		"authenticated", token != "")

	return nil
}

// Shutdown shuts down the GitHub domain plugin.
func (p *Plugin) Shutdown(ctx context.Context) error {
	logger := p.services.Logger("github")
	logger.Info("GitHub plugin shutting down")
	return nil
}

// RegisterHTTP registers HTTP handlers for the GitHub domain.
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	return nil // No HTTP endpoints needed yet
}

// RegisterWebSocket registers WebSocket handlers for the GitHub domain.
func (p *Plugin) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	return nil, nil // No WebSocket endpoints needed
}

// Health returns the health status of the GitHub domain plugin.
func (p *Plugin) Health(ctx context.Context) plugin.HealthStatus {
	return plugin.HealthStatus{
		Healthy: true,
		Paused:  p.IsPaused(),
		Message: p.HealthMessage("GitHub plugin"),
	}
}

// Pause temporarily suspends the GitHub plugin operations.
func (p *Plugin) Pause(ctx context.Context) error {
	if err := p.PauseController.Pause(); err != nil {
		return err
	}
	p.services.Logger("github").Info("GitHub plugin paused")
	return nil
}

// Resume restores the GitHub plugin to active operation.
func (p *Plugin) Resume(ctx context.Context) error {
	if err := p.PauseController.Resume(); err != nil {
		return err
	}
	p.services.Logger("github").Info("GitHub plugin resumed")
	return nil
}

// ConfigSchema returns the configuration schema for the GitHub plugin.
func (p *Plugin) ConfigSchema() map[string]plugin.ConfigField {
	return map[string]plugin.ConfigField{
		"token": {
			Type:        "string",
			Description: "GitHub personal access token for API authentication",
			Required:    false,
		},
		"repos": {
			Type:        "string[]",
			Description: "List of repositories to watch (format: owner/repo)",
			Required:    false,
		},
		"poll_interval": {
			Type:         "int",
			Description:  "Interval in seconds between polling GitHub API for events",
			DefaultValue: "300",
			Required:     false,
		},
	}
}

// GetSchedules returns the schedules this plugin wants QNTX to create.
// Called during initialization to auto-create Pulse scheduled jobs.
func (p *Plugin) GetSchedules() []*protocol.ScheduleInfo {
	config := p.services.Config("github")
	repos := config.GetStringSlice("repos")
	if len(repos) == 0 {
		// No repos configured, don't create schedule
		return nil
	}

	pollInterval := int32(config.GetInt("poll_interval"))
	if pollInterval <= 0 {
		pollInterval = 300 // 5 minutes default
	}

	return []*protocol.ScheduleInfo{
		{
			HandlerName:      "github.poll-events",
			IntervalSeconds:  pollInterval,
			EnabledByDefault: true,
			Description:      "Poll GitHub API for repository events",
		},
	}
}

// GetHandlerNames returns the async handler names this plugin can execute.
func (p *Plugin) GetHandlerNames() []string {
	return []string{"github.poll-events"}
}

// ExecuteJob executes an async job routed from Pulse.
func (p *Plugin) ExecuteJob(ctx context.Context, handlerName string, jobID string, payload []byte) (result []byte, err error) {
	switch handlerName {
	case "github.poll-events":
		// Execute GitHub event polling
		count, err := p.HandlePulseJob(ctx, jobID)
		if err != nil {
			return nil, err
		}

		// Return success result with attestation count
		resultData := map[string]interface{}{
			"attestations_created": count,
		}
		return json.Marshal(resultData)

	default:
		return nil, fmt.Errorf("unknown handler: %s", handlerName)
	}
}

// Verify Plugin implements required interfaces at compile time
var _ plugin.DomainPlugin = (*Plugin)(nil)
var _ plugin.PausablePlugin = (*Plugin)(nil)
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)

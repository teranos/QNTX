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
	plugin.Base
	client *GitHubClient // GitHub API client
}

// NewPlugin creates a new GitHub domain plugin.
func NewPlugin() *Plugin {
	return &Plugin{
		Base: plugin.NewBase(plugin.Metadata{
			Name:        "github",
			Version:     "0.1.7",
			QNTXVersion: ">= 0.1.0",
			Description: "GitHub integration for repository events and automation",
			Author:      "QNTX Team",
			License:     "MIT",
		}),
	}
}

// Initialize initializes the GitHub domain plugin.
func (p *Plugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	p.Init(services)
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

// RegisterHTTP registers HTTP handlers for the GitHub domain.
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	return nil // No HTTP endpoints needed yet
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
	config := p.Services().Config("github")
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
func (p *Plugin) ExecuteJob(ctx context.Context, handlerName string, jobID string, payload []byte) (result []byte, logs []*protocol.JobLogEntry, err error) {
	switch handlerName {
	case "github.poll-events":
		logs = append(logs, protocol.NewJobLogEntry("info", "poll-events", "Polling GitHub events"))

		count, err := p.HandlePulseJob(ctx, jobID)
		if err != nil {
			logs = append(logs, protocol.NewJobLogEntry("error", "poll-events", fmt.Sprintf("Poll failed: %v", err)))
			return nil, logs, err
		}

		logs = append(logs, protocol.NewJobLogEntry("info", "poll-events", fmt.Sprintf("Poll complete, %d attestations created", count)))

		resultData := map[string]interface{}{
			"attestations_created": count,
		}
		result, err := json.Marshal(resultData)
		return result, logs, err

	default:
		return nil, nil, protocol.ErrUnknownHandler(handlerName)
	}
}

// Verify Plugin implements required interfaces at compile time
var _ plugin.DomainPlugin = (*Plugin)(nil)
var _ plugin.PausablePlugin = (*Plugin)(nil)
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)

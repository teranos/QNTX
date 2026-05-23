package qntxopenrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// Plugin is the OpenRouter domain plugin implementation.
// Moves LLM prompt execution out of core into a separate gRPC process.
type Plugin struct {
	plugin.Base
	client *Client // OpenRouter API client
}

// NewPlugin creates a new OpenRouter domain plugin.
func NewPlugin() *Plugin {
	return &Plugin{
		Base: plugin.NewBase(plugin.Metadata{
			Name:        "openrouter",
			Version:     "0.4.3",
			QNTXVersion: ">= 0.1.0",
			Description: "OpenRouter LLM gateway for prompt execution, usage tracking, and model pricing",
			Author:      "QNTX Team",
			License:     "MIT",
		}),
	}
}

// Initialize initializes the OpenRouter domain plugin.
func (p *Plugin) Initialize(ctx context.Context, services plugin.ServiceRegistry) error {
	p.Init(services)
	logger := services.Logger("openrouter")

	config := services.Config("openrouter")
	apiKey := config.GetString("api_key")
	if apiKey == "" {
		logger.Warn("No OpenRouter API key configured - LLM calls will fail until configured")
	}

	model := config.GetString("model")
	if model == "" {
		model = DefaultModel
	}

	// Create OpenRouter client (usage tracking moved to core server)
	p.client = NewClient(Config{
		APIKey:        apiKey,
		Model:         model,
		Logger:        logger,
		OperationType: "prompt",
	})

	logger.Infow("OpenRouter plugin initialized",
		"model", model,
		"configured", apiKey != "")

	return nil
}

// RegisterHTTP registers HTTP handlers for prompt operations.
func (p *Plugin) RegisterHTTP(mux *http.ServeMux) error {
	h := &Handlers{plugin: p}

	mux.HandleFunc("/prompt/execute", h.HandlePromptExecute)
	mux.HandleFunc("/prompt/direct", h.HandlePromptDirect)
	mux.HandleFunc("/prompt/list", h.HandlePromptList)
	mux.HandleFunc("/prompt/save", h.HandlePromptSave)
	// Versioned prompts: /prompt/{name}/versions
	// Individual prompts: /prompt/{id}
	mux.HandleFunc("/prompt/", h.HandlePromptRoute)
	return nil
}

// ConfigSchema returns the configuration schema for the OpenRouter plugin.
func (p *Plugin) ConfigSchema() map[string]plugin.ConfigField {
	return map[string]plugin.ConfigField{
		"api_key": {
			Type:        "string",
			Description: "OpenRouter API key for authentication",
			Required:    false,
		},
		"model": {
			Type:         "string",
			Description:  "Default LLM model (e.g., openai/gpt-4o-mini)",
			DefaultValue: DefaultModel,
			Required:     false,
		},
	}
}

// GetHandlerNames returns the async handler names this plugin can execute.
func (p *Plugin) GetHandlerNames() []string {
	return []string{PromptExecuteHandlerName}
}

// ExecuteJob executes an async job routed from Pulse.
func (p *Plugin) ExecuteJob(ctx context.Context, handlerName string, jobID string, payload []byte) (result []byte, logs []*protocol.JobLogEntry, err error) {
	switch handlerName {
	case PromptExecuteHandlerName:
		logs = append(logs, protocol.NewJobLogEntry("info", "prompt", "Executing prompt via OpenRouter"))

		results, execErr := p.executePromptJob(ctx, jobID, payload)
		if execErr != nil {
			logs = append(logs, protocol.NewJobLogEntry("error", "prompt", fmt.Sprintf("Prompt execution failed: %v", execErr)))
			return nil, logs, execErr
		}

		logs = append(logs, protocol.NewJobLogEntry("info", "prompt", fmt.Sprintf("Prompt execution complete, %d results", len(results))))

		resultData, _ := json.Marshal(results)
		return resultData, logs, nil

	default:
		return nil, nil, protocol.ErrUnknownHandler(handlerName)
	}
}

// Chat implements plugin.LLMProvider — delegates to the existing OpenRouter client.
func (p *Plugin) Chat(ctx context.Context, req plugin.LLMRequest) (*plugin.LLMResponse, error) {
	// Convert plugin.LLMAttachment → ContentPart for the OpenRouter client
	var attachments []ContentPart
	for _, a := range req.Attachments {
		if a.MimeType == "application/pdf" || a.MimeType == "file" {
			// Anthropic document format — OpenRouter passes through to Anthropic models
			attachments = append(attachments, ContentPart{
				Type: "document",
				Source: &ContentPartSource{
					Type:      "base64",
					MediaType: "application/pdf",
					Data:      a.Data,
				},
			})
		} else {
			attachments = append(attachments, ContentPart{
				Type:     "image_url",
				ImageURL: &ContentPartImage{URL: fmt.Sprintf("data:%s;base64,%s", a.MimeType, a.Data)},
			})
		}
	}

	chatReq := ChatRequest{
		SystemPrompt: req.SystemPrompt,
		UserPrompt:   req.UserPrompt,
		Attachments:  attachments,
	}
	if req.Model != "" {
		chatReq.Model = &req.Model
	}
	if req.Temperature != 0 {
		chatReq.Temperature = &req.Temperature
	}
	if req.MaxTokens != 0 {
		chatReq.MaxTokens = &req.MaxTokens
	}

	resp, err := p.client.Chat(ctx, chatReq)
	if err != nil {
		return nil, err
	}

	return &plugin.LLMResponse{
		Content:          resp.Content,
		Model:            resp.Model,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}, nil
}

// Verify Plugin implements required interfaces at compile time
var _ plugin.DomainPlugin = (*Plugin)(nil)
var _ plugin.PausablePlugin = (*Plugin)(nil)
var _ plugin.ConfigurablePlugin = (*Plugin)(nil)
var _ plugin.LLMProvider = (*Plugin)(nil)

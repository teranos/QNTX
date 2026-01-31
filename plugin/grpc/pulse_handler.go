package grpc

import (
	"context"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
)

// PluginProxyHandler forwards job execution to a plugin via gRPC.
// This allows plugins to register async handlers without Go code changes.
//
// Architecture:
// - Pulse worker picks up job with handler_name "python.script"
// - PluginProxyHandler routes job to Python plugin via ExecuteJob RPC
// - Plugin executes script and returns result
// - Handler updates job state (progress, cost, error)
type PluginProxyHandler struct {
	handlerName string
	plugin      *ExternalDomainProxy
}

// NewPluginProxyHandler creates a handler that forwards execution to a plugin.
func NewPluginProxyHandler(handlerName string, plugin *ExternalDomainProxy) *PluginProxyHandler {
	return &PluginProxyHandler{
		handlerName: handlerName,
		plugin:      plugin,
	}
}

// Name returns the handler identifier (e.g., "python.script", "python.webhook").
func (h *PluginProxyHandler) Name() string {
	return h.handlerName
}

// Execute forwards the job to the plugin for execution.
func (h *PluginProxyHandler) Execute(ctx context.Context, job *async.Job) error {
	// Create ExecuteJob request
	req := &protocol.ExecuteJobRequest{
		JobId:       job.ID,
		HandlerName: h.handlerName,
		Payload:     job.Payload,
		TimeoutSecs: 300, // TODO: Make configurable or derive from job
	}

	// Forward to plugin via gRPC
	client := h.plugin.Client()
	resp, err := client.ExecuteJob(ctx, req)
	if err != nil {
		return errors.Wrapf(err, "plugin execution failed for handler %s", h.handlerName)
	}

	// Check if execution succeeded
	if !resp.Success {
		if resp.Error != "" {
			return errors.Newf("plugin execution error: %s", resp.Error)
		}
		return errors.New("plugin execution failed with no error message")
	}

	// Update job progress if provided by plugin
	if resp.ProgressTotal > 0 {
		job.Progress = async.Progress{
			Current: int(resp.ProgressCurrent),
			Total:   int(resp.ProgressTotal),
		}
	}

	// Update cost if provided by plugin
	if resp.CostActual > 0 {
		job.CostActual = resp.CostActual
	}

	// Execution succeeded
	return nil
}

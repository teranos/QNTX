// plugins/claude is qntx-claude-plugin: it runs Claude Code as a harness plugin (ADR-059): it
// declares harness_provider, and what it serves is A2A's A2AService, run by
// plugin/harness/claudeharness in the Claude Code it holds, pinned as the
// node's is and fetched under the plugin's own directory.
//
// "Harness is a plugin that speaks grpc A2A"
package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/plugin"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/harness/claudeharness"
	errors "github.com/teranos/sacred-error"
)

// pluginName is what the plugin is called, and the harness core routes to it by.
const pluginName = "claude"

type claudePlugin struct {
	mu     sync.Mutex
	binary string
	err    error
}

var _ plugingrpc.HarnessProvider = (*claudePlugin)(nil)

func (p *claudePlugin) Metadata() plugin.Metadata {
	return plugin.Metadata{
		Name:        pluginName,
		Version:     "0.1.0",
		QNTXVersion: ">= 0.1.0",
		Description: "Claude Code as a harness the node speaks to over A2A",
		Author:      "teranos",
		License:     "MIT",
	}
}

func (p *claudePlugin) Initialize(context.Context, plugin.ServiceRegistry) error { return nil }
func (p *claudePlugin) Shutdown(context.Context) error                            { return nil }
func (p *claudePlugin) RegisterHTTP(*http.ServeMux) error                         { return nil }
func (p *claudePlugin) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	return nil, nil
}

func (p *claudePlugin) Health(context.Context) plugin.HealthStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return plugin.HealthStatus{Healthy: false, Message: "no Claude Code to run: " + p.err.Error()}
	}
	return plugin.HealthStatus{Healthy: true, Message: "Claude Code harness", Details: map[string]any{"claude": p.binary}}
}

// Harness is Claude Code run by claudeharness, in the binary held returns.
func (p *claudePlugin) Harness() a2asrv.RequestHandler {
	return claudeharness.Handler(p.held)
}

// held is the pinned Claude Code under the plugin's own directory,
// ~/.qntx/plugins/claude, fetched the first time it is asked for.
func (p *claudePlugin) held(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.binary != "" {
		return p.binary, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		p.err = errors.Wrap(err, "the plugin has no home to keep Claude Code under")
		return "", p.err
	}
	pin, err := claudecode.Pinned()
	if err != nil {
		p.err = err
		return "", err
	}
	binary, err := pin.Ensure(ctx, filepath.Join(home, ".qntx", "plugins", pluginName))
	if err != nil {
		p.err = err
		return "", err
	}
	p.binary, p.err = binary, nil
	return binary, nil
}

func main() {
	plugingrpc.Run(&claudePlugin{}, 9010)
}

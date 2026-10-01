package server

// The node's GitHub settings, kept where the node keeps what it knows about
// itself (ADR-043). Each change is a new line and the newest line holds.

import (
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

const (
	// A GITHUB line is about the node's GitHub; its predicate says which part.
	githubSubject  = "GITHUB"
	githubNodeLine = "node"
)

// DefaultRunnerPath is where the Actions runner is installed on a box.
const DefaultRunnerPath = "/opt/actions-runner"

// GitHubSettings is the node's GitHub as the GitHub element sets it.
type GitHubSettings struct {
	Enabled       bool   `json:"enabled"`
	RunnerPath    string `json:"runner_path"`
	RunnerEnabled bool   `json:"runner_enabled"`
	// WebhookPath is where the App's webhook URL points on the node.
	WebhookPath string `json:"webhook_path"`
}

// NodeRecords reads and writes the GITHUB lines.
type NodeRecords struct{ s *QNTXServer }

func (s *QNTXServer) nodeRecords() NodeRecords { return NodeRecords{s: s} }

// newest is the newest line per predicate about subject.
func (r NodeRecords) newest(subject string) (map[string]*types.As, error) {
	if r.s.held == nil {
		return nil, errors.New("this node holds no store to keep its records in")
	}
	// Read where nodeRecord writes: system, or default on a backend with none.
	where := auth.NamespaceDefault
	if r.s.held.KeepsSystem() {
		where = auth.NamespaceSystem
	}
	reading, err := r.s.held.Read(where)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines in %s", subject, where)
	}
	found, err := reading.GetAttestations(ats.AttestationFilter{
		Subjects: []string{subject},
		Limit:    storage.MaxAttestationLimit,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the %s lines in %s", subject, where)
	}
	newest := map[string]*types.As{}
	for _, as := range found {
		if len(as.Predicates) == 0 {
			continue
		}
		name := as.Predicates[0]
		if held, ok := newest[name]; ok && !as.Timestamp.After(held.Timestamp) {
			continue
		}
		newest[name] = as
	}
	return newest, nil
}

// nodeRecord writes one line about the node.
func (r NodeRecords) nodeRecord(actor, subject, predicate, context string, attributes map[string]any) error {
	store := r.s.held.TheNodesOwnRecords()
	id, err := identity.GenerateASUIDWithRetry("AS", subject, predicate, context, store.AttestationExists)
	if err != nil {
		return errors.Wrapf(err, "failed to name the %s line about %s", subject, predicate)
	}
	at := time.Now()
	if err := store.CreateAttestation(&types.As{
		ID: id, Subjects: []string{subject}, Predicates: []string{predicate}, Contexts: []string{context},
		Actors: []string{actor}, Timestamp: at, CreatedAt: at, Source: pluginSource, Attributes: attributes,
	}); err != nil {
		return errors.Wrapf(err, "the %s line about %s was not written", subject, predicate)
	}
	measure.Count(measure.AttestationsWritten, 1)
	return nil
}

// GitHub is the node's GitHub. On until somebody switches it off; the runner
// is off until somebody switches it on.
func (r NodeRecords) GitHub() (GitHubSettings, error) {
	settings := GitHubSettings{Enabled: true, RunnerPath: DefaultRunnerPath, WebhookPath: githubWebhookPath}
	newest, err := r.newest(githubSubject)
	if err != nil {
		return settings, err
	}
	as, ok := newest[githubNodeLine]
	if !ok {
		return settings, nil
	}
	if enabled, ok := as.Attributes["enabled"].(bool); ok {
		settings.Enabled = enabled
	}
	if path, ok := as.Attributes["runner_path"].(string); ok && path != "" {
		settings.RunnerPath = path
	}
	if enabled, ok := as.Attributes["runner_enabled"].(bool); ok {
		settings.RunnerEnabled = enabled
	}
	if path, ok := as.Attributes["webhook_path"].(string); ok && path != "" {
		settings.WebhookPath = path
	}
	return settings, nil
}

// SetGitHub writes the node's GitHub settings whole.
func (r NodeRecords) SetGitHub(actor string, settings GitHubSettings) error {
	return r.nodeRecord(actor, githubSubject, githubNodeLine, "_", map[string]any{
		"enabled":        settings.Enabled,
		"runner_path":    settings.RunnerPath,
		"runner_enabled": settings.RunnerEnabled,
		"webhook_path":   settings.WebhookPath,
	})
}

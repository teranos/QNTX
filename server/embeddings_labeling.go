//go:build cgo && rustembeddings

package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
	"unicode/utf8"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/attrs"
	"github.com/teranos/QNTX/ats/identity"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

const ClusterLabelHandlerName = "embeddings.cluster-label"
const clusterLabelSystemPrompt = "You label clusters of text. Given sample texts from a cluster, respond with a short descriptive label (2-5 words). No explanation, just the label."

// ClusterLabelHandler asks an LLM to label unlabeled clusters.
type ClusterLabelHandler struct {
	server   *QNTXServer
	db       *sql.DB
	store    *storage.EmbeddingStore
	atsStore ats.AttestationStore
	cfg      *appcfg.Config
	logger   *zap.SugaredLogger
}

func (h *ClusterLabelHandler) Name() string { return ClusterLabelHandlerName }

func (h *ClusterLabelHandler) Execute(ctx context.Context, job *async.Job) error {
	embCfg := h.cfg.Embeddings
	minSize := embCfg.ClusterLabelMinSize
	cooldownDays := embCfg.ClusterLabelCooldownDays
	maxPerCycle := embCfg.ClusterLabelMaxPerCycle
	sampleSize := embCfg.ClusterLabelSampleSize
	maxTokens := embCfg.ClusterLabelMaxTokens

	eligible, err := h.store.GetLabelEligibleClusters(minSize, cooldownDays, maxPerCycle)
	if err != nil {
		h.writeLog(job.ID, "labeling", "error", fmt.Sprintf("Failed to query eligible clusters: %s", err), "")
		return err
	}

	if len(eligible) == 0 {
		h.writeLog(job.ID, "labeling", "info", "No clusters eligible for labeling", "")
		return nil
	}

	h.writeLog(job.ID, "labeling", "info",
		fmt.Sprintf("Found %d eligible clusters", len(eligible)),
		fmt.Sprintf(`{"eligible":%d,"min_size":%d,"cooldown_days":%d}`, len(eligible), minSize, cooldownDays))

	modelOverride := embCfg.ClusterLabelModel
	asStore := h.atsStore

	labeled := 0
	for _, cluster := range eligible {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		samples, err := h.store.SampleClusterTexts(cluster.ID, sampleSize)
		if err != nil {
			h.logger.Warnw("Failed to sample texts for cluster",
				"cluster_id", cluster.ID, "error", err)
			continue
		}
		if len(samples) == 0 {
			continue
		}

		// Build prompt
		var userPrompt strings.Builder
		fmt.Fprintf(&userPrompt, "Cluster %d (%d members). Sample texts:\n", cluster.ID, cluster.Members)
		for i, text := range samples {
			fmt.Fprintf(&userPrompt, "%d. %s\n", i+1, text)
		}

		resp, err := h.callPromptDirect(ctx, userPrompt.String(), modelOverride, maxTokens)
		if err != nil {
			h.logger.Warnw("LLM labeling failed for cluster",
				"cluster_id", cluster.ID, "error", err)
			h.writeLog(job.ID, "labeling", "error",
				fmt.Sprintf("LLM failed for cluster %d: %s", cluster.ID, err), "")
			continue
		}

		// Clean label: trim whitespace, enforce max length (rune-safe)
		label := strings.TrimSpace(resp.Response)
		if utf8.RuneCountInString(label) > 100 {
			runes := []rune(label)
			label = string(runes[:100])
		}
		if label == "" {
			h.logger.Warnw("LLM returned empty label for cluster", "cluster_id", cluster.ID)
			continue
		}

		modelUsed := resp.Model
		if modelUsed == "" {
			modelUsed = modelOverride
		}

		if err := h.store.UpdateClusterLabel(cluster.ID, label); err != nil {
			h.logger.Warnw("Failed to update cluster label",
				"cluster_id", cluster.ID, "error", err)
			continue
		}

		h.createLabelAttestation(asStore, cluster.ID, label, modelUsed, len(samples), cluster.Members)

		labeled++
		labelJSON, _ := json.Marshal(label)
		h.writeLog(job.ID, "labeling", "info",
			fmt.Sprintf("Labeled cluster %d: %q (%d members, %d samples)",
				cluster.ID, label, cluster.Members, len(samples)),
			fmt.Sprintf(`{"cluster_id":%d,"label":%s,"members":%d,"samples":%d}`,
				cluster.ID, labelJSON, cluster.Members, len(samples)))
	}

	h.writeLog(job.ID, "labeling", "info",
		fmt.Sprintf("Labeling complete: %d/%d clusters labeled", labeled, len(eligible)),
		fmt.Sprintf(`{"labeled":%d,"eligible":%d}`, labeled, len(eligible)))
	return nil
}

// clusterLabelAttrs defines the attribute schema for cluster label attestations.
type clusterLabelAttrs struct {
	Label      string `attr:"label"`
	Model      string `attr:"model"`
	SampleSize int    `attr:"sample_size"`
	NMembers   int    `attr:"n_members"`
}

func (h *ClusterLabelHandler) createLabelAttestation(asStore ats.AttestationStore, clusterID int, label, model string, sampleSize, nMembers int) {
	subject := fmt.Sprintf("cluster:%d", clusterID)
	asid, err := identity.GenerateASUID("AS", subject, "labeled", "embeddings")
	if err != nil {
		h.logger.Warnw("Failed to generate ASID for label attestation",
			"cluster_id", clusterID, "error", err)
		return
	}

	now := time.Now()
	as := &types.As{
		ID:         asid,
		Subjects:   []string{subject},
		Predicates: []string{"labeled"},
		Contexts:   []string{"embeddings"},
		Actors:     []string{"qntx@embeddings"},
		Timestamp:  now,
		Source:     "cluster-labeling",
		Attributes: attrs.From(clusterLabelAttrs{
			Label:      label,
			Model:      model,
			SampleSize: sampleSize,
			NMembers:   nMembers,
		}),
		CreatedAt: now,
	}

	if err := asStore.CreateAttestation(as); err != nil {
		h.logger.Warnw("Failed to create label attestation",
			"cluster_id", clusterID, "asid", asid, "error", err)
	} else {
		h.logger.Infow("Created label attestation",
			"asid", asid, "cluster_id", clusterID, "label", label)
	}
}

// callPromptDirect routes an LLM request through HandlePromptDirect, which
// handles plugin forwarding (OpenRouter) and local inference transparently.
func (h *ClusterLabelHandler) callPromptDirect(ctx context.Context, userPrompt, model string, maxTokens int) (*PromptDirectResponse, error) {
	template := fmt.Sprintf("---\nmax_tokens: %d\n", maxTokens)
	if model != "" {
		template += fmt.Sprintf("model: %q\n", model)
	}
	template += "---\n" + userPrompt

	reqBody := PromptDirectRequest{
		Template:     template,
		SystemPrompt: clusterLabelSystemPrompt,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal prompt request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/prompt/direct", io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create internal HTTP request")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.ContentLength = int64(len(body))

	rec := httptest.NewRecorder()
	h.server.HandlePromptDirect(rec, httpReq)

	if rec.Code != http.StatusOK {
		return nil, errors.Newf("prompt/direct returned status %d: %s", rec.Code, rec.Body.String())
	}

	var resp PromptDirectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		return nil, errors.Wrap(err, "failed to parse prompt response")
	}
	if resp.Error != "" {
		return nil, errors.Newf("prompt/direct error: %s", resp.Error)
	}

	return &resp, nil
}

func (h *ClusterLabelHandler) writeLog(jobID, stage, level, message, metadata string) {
	var metaPtr *string
	if metadata != "" {
		metaPtr = &metadata
	}
	_, err := h.db.Exec(`INSERT INTO task_logs (job_id, stage, timestamp, level, message, metadata) VALUES (?, ?, ?, ?, ?, ?)`,
		jobID, stage, time.Now().Format(time.RFC3339), level, message, metaPtr)
	if err != nil {
		h.logger.Warnw("Failed to write task log", "job_id", jobID, "error", err)
	}
}

// setupClusterLabelSchedule registers the cluster label handler and auto-creates
// a Pulse schedule if embeddings.cluster_label_interval_seconds > 0.
func (s *QNTXServer) setupClusterLabelSchedule(cfg *appcfg.Config) {
	if s.embeddingStore == nil {
		return
	}

	handler := &ClusterLabelHandler{
		server:   s,
		db:       s.db,
		store:    s.embeddingStore,
		atsStore: s.atsStore,
		cfg:      cfg,
		logger:   s.logger.Named("cluster-label"),
	}

	registry := s.daemon.Registry()
	registry.Register(handler)
	s.logger.Infow("Registered cluster label handler")

	schedStore := schedule.NewStore(s.db)

	// If interval not configured, pause any existing active schedule
	if cfg.Embeddings.ClusterLabelIntervalSeconds == nil {
		s.pauseExistingSchedule(schedStore, ClusterLabelHandlerName)
		return
	}
	interval := *cfg.Embeddings.ClusterLabelIntervalSeconds

	// Check for existing schedule to avoid duplicates on restart
	existing, err := schedStore.ListAllScheduledJobs()
	if err != nil {
		s.logger.Errorw("Failed to list scheduled jobs for cluster-label idempotency check",
			"handler_name", ClusterLabelHandlerName,
			"error", err)
		return
	}
	for _, j := range existing {
		if j.HandlerName == ClusterLabelHandlerName && j.State == schedule.StateActive {
			if j.IntervalSeconds != interval {
				if err := schedStore.UpdateJobInterval(j.ID, interval); err != nil {
					s.logger.Errorw("Failed to update cluster-label schedule interval",
						"job_id", j.ID,
						"error", err)
				} else {
					s.logger.Infow("Updated cluster-label schedule interval",
						"job_id", j.ID,
						"interval_seconds", interval)
				}
			}
			return
		}
	}

	now := time.Now()
	job := &schedule.Job{
		ID:              fmt.Sprintf("SPJ_cluster_label_%d", now.Unix()),
		HandlerName:     ClusterLabelHandlerName,
		IntervalSeconds: interval,
		State:           schedule.StateActive,
	}

	// Only run immediately if there are enough embeddings for clusters to exist
	count, err := s.embeddingStore.CountEmbeddings()
	if err == nil && count >= 2 {
		job.NextRunAt = &now
	}

	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create cluster-label schedule",
			"interval_seconds", interval,
			"error", err)
		return
	}
	s.logger.Infow("Auto-created cluster-label schedule",
		"job_id", job.ID,
		"interval_seconds", interval,
		"embedding_count", count)
}

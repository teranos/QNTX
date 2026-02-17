//go:build cgo && rustembeddings

package server

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/teranos/QNTX/ai/openrouter"
	"github.com/teranos/QNTX/ai/provider"
	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
	vanity "github.com/teranos/vanity-id"
	"go.uber.org/zap"
)

const ClusterLabelHandlerName = "embeddings.cluster-label"

// ClusterLabelHandler asks an LLM to label unlabeled clusters.
type ClusterLabelHandler struct {
	db     *sql.DB
	store  *storage.EmbeddingStore
	cfg    *appcfg.Config
	logger *zap.SugaredLogger
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

	labeled := 0
	for _, cluster := range eligible {
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

		// Create AI client
		aiProvider := provider.DetermineProvider(h.cfg, "")
		modelOverride := embCfg.ClusterLabelModel
		client := provider.NewAIClientForProviderWithModel(
			aiProvider, h.cfg, modelOverride, h.db, 0,
			"cluster-labeling", "cluster", fmt.Sprintf("%d", cluster.ID),
		)

		req := openrouter.ChatRequest{
			SystemPrompt: "You label clusters of text. Given sample texts from a cluster, respond with a short descriptive label (2-5 words). No explanation, just the label.",
			UserPrompt:   userPrompt.String(),
			MaxTokens:    &maxTokens,
		}

		resp, err := client.Chat(ctx, req)
		if err != nil {
			h.logger.Warnw("LLM labeling failed for cluster",
				"cluster_id", cluster.ID, "error", err)
			h.writeLog(job.ID, "labeling", "error",
				fmt.Sprintf("LLM failed for cluster %d: %s", cluster.ID, err), "")
			continue
		}

		// Clean label: trim whitespace, enforce max length
		label := strings.TrimSpace(resp.Content)
		if len(label) > 100 {
			label = label[:100]
		}
		if label == "" {
			h.logger.Warnw("LLM returned empty label for cluster", "cluster_id", cluster.ID)
			continue
		}

		if err := h.store.UpdateClusterLabel(cluster.ID, label); err != nil {
			h.logger.Warnw("Failed to update cluster label",
				"cluster_id", cluster.ID, "error", err)
			continue
		}

		// Determine which model was actually used
		modelUsed := modelOverride
		if modelUsed == "" {
			modelUsed = h.cfg.OpenRouter.Model
			if h.cfg.LocalInference.Enabled {
				modelUsed = h.cfg.LocalInference.Model
			}
		}

		// Create attestation for the labeling event
		h.createLabelAttestation(cluster.ID, label, modelUsed, len(samples), cluster.Members)

		labeled++
		h.writeLog(job.ID, "labeling", "info",
			fmt.Sprintf("Labeled cluster %d: %q (%d members, %d samples)",
				cluster.ID, label, cluster.Members, len(samples)),
			fmt.Sprintf(`{"cluster_id":%d,"label":"%s","members":%d,"samples":%d}`,
				cluster.ID, label, cluster.Members, len(samples)))
	}

	h.writeLog(job.ID, "labeling", "info",
		fmt.Sprintf("Labeling complete: %d/%d clusters labeled", labeled, len(eligible)),
		fmt.Sprintf(`{"labeled":%d,"eligible":%d}`, labeled, len(eligible)))
	return nil
}

func (h *ClusterLabelHandler) createLabelAttestation(clusterID int, label, model string, sampleSize, nMembers int) {
	subject := fmt.Sprintf("cluster:%d", clusterID)
	asid, err := vanity.GenerateASID(subject, "labeled", "embeddings", "qntx@embeddings")
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
		Attributes: map[string]interface{}{
			"label":       label,
			"model":       model,
			"sample_size": sampleSize,
			"n_members":   nMembers,
		},
		CreatedAt: now,
	}

	store := storage.NewSQLStore(h.db, h.logger)
	if err := store.CreateAttestation(as); err != nil {
		h.logger.Warnw("Failed to create label attestation",
			"cluster_id", clusterID, "asid", asid, "error", err)
	} else {
		h.logger.Infow("Created label attestation",
			"asid", asid, "cluster_id", clusterID, "label", label)
	}
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
		db:     s.db,
		store:  s.embeddingStore,
		cfg:    cfg,
		logger: s.logger.Named("cluster-label"),
	}

	registry := s.daemon.Registry()
	registry.Register(handler)
	s.logger.Infow("Registered cluster label handler")

	interval := cfg.Embeddings.ClusterLabelIntervalSeconds
	if interval <= 0 {
		return
	}

	// Check for existing schedule to avoid duplicates on restart
	schedStore := schedule.NewStore(s.db)
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
		NextRunAt:       &now,
	}
	if err := schedStore.CreateJob(job); err != nil {
		s.logger.Errorw("Failed to create cluster-label schedule",
			"interval_seconds", interval,
			"error", err)
		return
	}
	s.logger.Infow("Auto-created cluster-label schedule",
		"job_id", job.ID,
		"interval_seconds", interval)
}

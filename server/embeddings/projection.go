package embeddings

import (
	"context"
	"encoding/json"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/errors"
	"go.uber.org/zap"
)

// ProjectionResult holds the outcome of a single-method projection run.
type ProjectionResult struct {
	Method  string  `json:"method"`
	NPoints int     `json:"n_points"`
	FitMS   int64   `json:"fit_ms"`
	TimeMS  float64 `json:"time_ms"`
}

// ProjectionParams holds per-method tuning parameters for dimensionality reduction.
type ProjectionParams struct {
	NComponents *int     `json:"n_components,omitempty"` // Output dimensions (default 3)
	NNeighbors  *int     `json:"n_neighbors,omitempty"`  // UMAP: local vs global (default 15)
	MinDist     *float64 `json:"min_dist,omitempty"`     // UMAP: cluster tightness (default 0.1)
	Perplexity  *float64 `json:"perplexity,omitempty"`   // t-SNE: local vs global (default 30)
}

// validProjectionMethods filters unknown methods, logging warnings for skipped ones.
var knownMethods = map[string]bool{"umap": true, "tsne": true, "pca": true}

func validProjectionMethods(methods []string, logger *zap.SugaredLogger) []string {
	var valid []string
	for _, m := range methods {
		if knownMethods[m] {
			valid = append(valid, m)
		} else {
			logger.Warnw("Skipping unknown projection method", "method", m)
		}
	}
	return valid
}

// RunProjection reads all embeddings, calls the reduce plugin /fit for the given method,
// and writes projections (2D or 3D) to DB.
func RunProjection(
	ctx context.Context,
	method string,
	store *storage.EmbeddingStore,
	svc EmbeddingServiceForClustering,
	callReduce ReduceFunc,
	logger *zap.SugaredLogger,
	params *ProjectionParams,
) (*ProjectionResult, error) {
	startTime := time.Now()

	ids, blobs, err := store.GetAllEmbeddingVectors()
	if err != nil {
		return nil, errors.Wrap(err, "failed to read embedding vectors for projection")
	}

	if len(ids) < 2 {
		return nil, errors.Newf("need at least 2 embeddings to project, have %d", len(ids))
	}

	allEmbeddings := make([][]float32, 0, len(blobs))
	for i, blob := range blobs {
		vec, err := svc.DeserializeEmbedding(blob)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to deserialize embedding %s", ids[i])
		}
		allEmbeddings = append(allEmbeddings, vec)
	}

	fitBody := map[string]any{
		"embeddings":   allEmbeddings,
		"method":       method,
		"n_components": 3,
	}
	if params != nil {
		if params.NComponents != nil {
			fitBody["n_components"] = *params.NComponents
		}
		if params.NNeighbors != nil {
			fitBody["n_neighbors"] = *params.NNeighbors
		}
		if params.MinDist != nil {
			fitBody["min_dist"] = *params.MinDist
		}
		if params.Perplexity != nil {
			fitBody["perplexity"] = *params.Perplexity
		}
	}
	fitReq, err := json.Marshal(fitBody)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal fit request")
	}

	fitResp, err := callReduce(ctx, "POST", "/fit", fitReq)
	if err != nil {
		return nil, errors.Wrapf(err, "reduce plugin /fit failed for method %s (%d points)", method, len(ids))
	}

	var fitResult struct {
		Projections [][]float64 `json:"projections"`
		NComponents int         `json:"n_components"`
		NPoints     int         `json:"n_points"`
		FitMS       int64       `json:"fit_ms"`
	}
	if err := json.Unmarshal(fitResp, &fitResult); err != nil {
		return nil, errors.Wrapf(err, "failed to parse reduce plugin response for method %s", method)
	}

	if len(fitResult.Projections) != len(ids) {
		return nil, errors.Newf("projection count mismatch for %s: got %d, expected %d",
			method, len(fitResult.Projections), len(ids))
	}

	assignments := make([]storage.ProjectionAssignment, len(ids))
	for i, id := range ids {
		p := fitResult.Projections[i]
		a := storage.ProjectionAssignment{
			ID: id,
			X:  p[0],
			Y:  p[1],
		}
		if len(p) >= 3 {
			z := p[2]
			a.Z = &z
		}
		assignments[i] = a
	}

	if err := store.UpdateProjections(method, assignments); err != nil {
		return nil, errors.Wrapf(err, "failed to save %s projections for %d points", method, len(assignments))
	}

	totalMS := float64(time.Since(startTime).Milliseconds())

	logger.Infow("Projection complete",
		"method", method,
		"n_points", len(ids),
		"fit_ms", fitResult.FitMS,
		"total_ms", totalMS)

	return &ProjectionResult{
		Method:  method,
		NPoints: len(ids),
		FitMS:   fitResult.FitMS,
		TimeMS:  totalMS,
	}, nil
}

// RunAllProjections runs projection sequentially for each configured method.
func RunAllProjections(
	ctx context.Context,
	methods []string,
	store *storage.EmbeddingStore,
	svc EmbeddingServiceForClustering,
	callReduce ReduceFunc,
	logger *zap.SugaredLogger,
	params *ProjectionParams,
) ([]ProjectionResult, error) {
	var results []ProjectionResult
	validated := validProjectionMethods(methods, logger)
	for _, method := range validated {
		result, err := RunProjection(ctx, method, store, svc, callReduce, logger, params)
		if err != nil {
			return results, errors.Wrapf(err, "projection failed for method %s", method)
		}
		results = append(results, *result)
	}
	return results, nil
}

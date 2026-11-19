package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// Embedding as signa: the service a plugin provides (ADR-017), and what the
// node keeps of what it embedded, clustered and projected. Every route of it
// is a sigil, so a subsystem asks it as a person or an agent does.

func (s *QNTXServer) embeddingsSignum() sigil.Signum {
	model := &protocol.Param{Name: "model", Says: "The model, by name; none is the embedding provider's first."}
	clusterID := &protocol.Param{Name: "cluster_id", Required: true, Says: "The cluster, by its id."}
	limit := &protocol.Param{Name: "limit", Kind: sigil.Count, Says: "How many to answer at most."}
	sigils := []*protocol.Sigil{
		{Name: "generate", Does: "Text as a vector, by a model.",
			Takes:  []*protocol.Param{{Name: "text", Required: true, Says: "The text to embed."}, model},
			Answer: "protocol.EmbedResponse", Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/embeddings/generate"}},
		{Name: "generate batch", Does: "Texts as vectors, by a model, in the order they were sent.",
			Takes:  []*protocol.Param{{Name: "texts", Required: true, Kind: sigil.List, Says: "The texts to embed, a list."}, model},
			Answer: "protocol.BatchEmbedResponse", Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/embeddings/generate/batch"}},
		{Name: "batch", Does: "Embed attestations by their ids and keep what was embedded, with how many were and were not.",
			Takes: []*protocol.Param{{Name: "attestation_ids", Required: true, Kind: sigil.List, Says: "The attestations to embed, by id."}},
			Http:  &protocol.Endpoint{Method: http.MethodPost, Path: "/api/embeddings/batch"}},
		{Name: "cluster", Does: "Cluster every kept embedding of a model with HDBSCAN, matched against the clusters before so a cluster keeps its id.",
			Takes: []*protocol.Param{model,
				{Name: "min_cluster_size", Kind: sigil.Count, Says: "The fewest members a cluster holds."},
				{Name: "cluster_threshold", Says: "How similar a new embedding is to a centroid to be put in its cluster."},
				{Name: "cluster_match_threshold", Says: "How similar a cluster is to one before to keep its id."}},
			Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/embeddings/cluster"}},
		{Name: "clusters", Does: "The clusters that stand: each with its label, members, status and when it was first and last seen.",
			Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/embeddings/clusters"}},
		{Name: "samples", Does: "Texts sampled from one cluster.",
			Takes: []*protocol.Param{clusterID, {Name: "size", Kind: sigil.Count, Says: "How many texts to sample."}},
			Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/embeddings/clusters/samples"}},
		{Name: "members", Does: "The newest attestations in one cluster.",
			Takes: []*protocol.Param{clusterID, limit},
			Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/embeddings/clusters/members"}},
		{Name: "memberships", Does: "Which cluster each of some attestations is in.",
			Takes: []*protocol.Param{{Name: "ids", Required: true, Says: "The attestations, by id, separated by commas."}},
			Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/embeddings/clusters/memberships"}},
		{Name: "by-source", Does: "The kept embeddings of some attestations: each vector with its model and dimensions.",
			Takes: []*protocol.Param{{Name: "source_ids", Required: true, Kind: sigil.List, Says: "The attestations, by id."}},
			Http:  &protocol.Endpoint{Method: http.MethodPost, Path: "/api/embeddings/by-source"}},
		{Name: "timeline", Does: "How the clusters changed, run by run.",
			Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/embeddings/cluster-timeline"}},
		{Name: "info", Does: "Whether embedding is served, by which models, how much is embedded and how far behind it is.",
			Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/embeddings/info"}},
		{Name: "unembedded", Does: "Attestations with text that nothing has embedded yet, by id.",
			Takes: []*protocol.Param{limit},
			Http:  &protocol.Endpoint{Method: http.MethodGet, Path: "/api/embeddings/unembedded"}},
		{Name: "project", Does: "Project every kept embedding of a model into two dimensions, by each method the node is set to.",
			Takes: []*protocol.Param{model,
				{Name: "n_neighbors", Says: "UMAP's neighbours."},
				{Name: "min_dist", Says: "UMAP's least distance."},
				{Name: "perplexity", Says: "t-SNE's perplexity."}},
			Http: &protocol.Endpoint{Method: http.MethodPost, Path: "/api/embeddings/project"}},
		{Name: "projections", Does: "The kept embeddings in two dimensions, by method, with their clusters.",
			Http: &protocol.Endpoint{Method: http.MethodGet, Path: "/api/embeddings/projections"}},
	}
	for _, held := range sigils {
		held.Surfaces = providedSurfaces
	}
	handled := func(name, method, path string) sigil.Answer {
		return s.throughHandler(name, method, path)
	}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "embeddings",
			Description: "Embedding, a service a plugin provides (ADR-017), and what the node keeps of what it embedded, clustered and projected.",
			Tags:        []string{"embeddings", "vectors", "clusters"},
			Sigils:      sigils,
		},
		Answers: map[string]sigil.Answer{
			"generate":       s.embeddingsGenerate,
			"generate batch": s.embeddingsBatch,
			"batch":          handled("batch", http.MethodPost, "/api/embeddings/batch"),
			"cluster":        handled("cluster", http.MethodPost, "/api/embeddings/cluster"),
			"clusters":       handled("clusters", http.MethodGet, "/api/embeddings/clusters"),
			"samples":        handled("samples", http.MethodGet, "/api/embeddings/clusters/samples"),
			"members":        handled("members", http.MethodGet, "/api/embeddings/clusters/members"),
			"memberships":    handled("memberships", http.MethodGet, "/api/embeddings/clusters/memberships"),
			"by-source":      handled("by-source", http.MethodPost, "/api/embeddings/by-source"),
			"timeline":       handled("timeline", http.MethodGet, "/api/embeddings/cluster-timeline"),
			"info":           handled("info", http.MethodGet, "/api/embeddings/info"),
			"unembedded":     handled("unembedded", http.MethodGet, "/api/embeddings/unembedded"),
			"project":        handled("project", http.MethodPost, "/api/embeddings/project"),
			"projections":    handled("projections", http.MethodGet, "/api/embeddings/projections"),
		},
	}
}

func (s *QNTXServer) searchSignum() sigil.Signum {
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "search",
			Description: "Attestations found by what their text means: the query embedded, its nearest kept embeddings found by vector search.",
			Tags:        []string{"search", "semantic", "embeddings"},
			Sigils: []*protocol.Sigil{{
				Name: "semantic",
				Does: "The attestations whose text is nearest in meaning to a query, as similar as a threshold or more, within the caller's read scope.",
				Takes: []*protocol.Param{
					{Name: "q", Required: true, Says: "What to search for, in words."},
					{Name: "limit", Kind: sigil.Count, Says: "How many to answer at most; 10 when none."},
					{Name: "threshold", Says: "The least similarity, from 0 to 1; 0.7 when none."},
					{Name: "cluster_id", Says: "Only within this cluster, by its id."},
				},
				Http:     &protocol.Endpoint{Method: http.MethodGet, Path: "/api/search/semantic"},
				Surfaces: providedSurfaces,
			}},
		},
		Answers: map[string]sigil.Answer{"semantic": s.throughHandler("semantic", http.MethodGet, "/api/search/semantic")},
	}
}

// embeddingsRoute is the handler that answered a route of embeddings before it
// was a sigil, by the sigil's name, or nil before the handler is made.
func (s *QNTXServer) embeddingsRoute(name string) http.HandlerFunc {
	h := s.embeddingsHandler
	if h == nil {
		return nil
	}
	return map[string]http.HandlerFunc{
		"batch":       h.HandleEmbeddingBatch,
		"cluster":     h.HandleCluster,
		"clusters":    h.HandleEmbeddingClusters,
		"samples":     h.HandleClusterSamples,
		"members":     h.HandleClusterMembers,
		"memberships": h.HandleClusterMemberships,
		"by-source":   h.HandleEmbeddingsBySource,
		"timeline":    h.HandleClusterTimeline,
		"info":        h.HandleEmbeddingInfo,
		"unembedded":  h.HandleUnembeddedPage,
		"project":     h.HandleProject,
		"projections": h.HandleEmbeddingProjections,
		"semantic":    h.HandleSemanticSearch,
	}[name]
}

// throughHandler answers a sigil by the handler that answered its route
// before it was one: what arrived goes in as the query, and as the JSON body
// where the method carries one, with who is asking in the context; what the
// handler wrote is the answer, and what it refused with is the refusal.
func (s *QNTXServer) throughHandler(name, method, path string) sigil.Answer {
	return func(ctx context.Context, _ sigil.Sent) (any, *protocol.Refusal) {
		handler := s.embeddingsRoute(name)
		if handler == nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: "embeddings are not served on this node yet"}
		}
		arrived := sigil.Arrived(ctx)
		query := url.Values{}
		for key, value := range arrived {
			switch v := value.(type) {
			case string:
				query.Set(key, v)
			case float64:
				query.Set(key, strconv.FormatFloat(v, 'f', -1, 64))
			case bool:
				query.Set(key, strconv.FormatBool(v))
			case int:
				query.Set(key, strconv.Itoa(v))
			}
		}
		var body io.Reader = http.NoBody
		if carriesBody(method) {
			raw, err := json.Marshal(arrived)
			if err != nil {
				return nil, &protocol.Refusal{Why: sigil.Invalid, Says: "what was sent to " + name + " does not write as JSON: " + err.Error()}
			}
			body = bytes.NewReader(raw)
		}
		target := path
		if encoded := query.Encode(); encoded != "" {
			target += "?" + encoded
		}
		req, err := http.NewRequestWithContext(ctx, method, target, body)
		if err != nil {
			return nil, &protocol.Refusal{Why: sigil.Failed, Says: name + " could not be asked: " + err.Error()}
		}
		req.Header.Set("Content-Type", "application/json")
		written := &handlerWrote{header: http.Header{}}
		handler(written, req)
		says := strings.TrimSpace(written.body.String())
		switch {
		case written.status == 0 || written.status < 300:
			if says == "" {
				return nil, nil
			}
			return json.RawMessage(written.body.Bytes()), nil
		case written.status == http.StatusNotFound:
			return nil, &protocol.Refusal{Why: sigil.NotFound, Says: says}
		case written.status == http.StatusForbidden:
			return nil, &protocol.Refusal{Why: sigil.NotAllowed, Says: says}
		case written.status < 500:
			return nil, &protocol.Refusal{Why: sigil.Invalid, Says: says}
		}
		return nil, &protocol.Refusal{Why: sigil.Failed, Says: says}
	}
}

// handlerWrote holds what a handler wrote, to be handed on as an answer.
type handlerWrote struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *handlerWrote) Header() http.Header { return w.header }

func (w *handlerWrote) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *handlerWrote) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(b)
}

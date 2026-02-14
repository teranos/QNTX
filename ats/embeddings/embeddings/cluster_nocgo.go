//go:build !cgo || !rustembeddings

package embeddings

import "github.com/teranos/QNTX/errors"

// ClusterResult holds the output of HDBSCAN clustering.
type ClusterResult struct {
	Labels        []int32   `json:"labels"`
	Probabilities []float32 `json:"probabilities"`
	NClusters     int       `json:"n_clusters"`
	NPoints       int       `json:"n_points"`
	NNoise        int       `json:"n_noise"`
}

// ClusterHDBSCAN is a stub that returns an error when built without rustembeddings.
func ClusterHDBSCAN(data []float32, nPoints, dimensions, minClusterSize int) (*ClusterResult, error) {
	return nil, errors.New("HDBSCAN clustering not available: built without rustembeddings support")
}

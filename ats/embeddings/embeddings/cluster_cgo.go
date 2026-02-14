//go:build cgo && rustembeddings

package embeddings

/*
#cgo LDFLAGS: -L${SRCDIR}/../../../target/release -lqntx_embeddings
#cgo darwin LDFLAGS: -framework CoreFoundation -framework Security -lresolv
#cgo CFLAGS: -I${SRCDIR}/../include

#include <stdlib.h>
#include "embeddings.h"
*/
import "C"
import (
	"unsafe"

	"github.com/teranos/QNTX/errors"
)

// ClusterResult holds the output of HDBSCAN clustering.
type ClusterResult struct {
	Labels        []int32   `json:"labels"`
	Probabilities []float32 `json:"probabilities"`
	NClusters     int       `json:"n_clusters"`
	NPoints       int       `json:"n_points"`
	NNoise        int       `json:"n_noise"`
}

// ClusterHDBSCAN runs HDBSCAN clustering on a flat array of float32 embeddings.
func ClusterHDBSCAN(data []float32, nPoints, dimensions, minClusterSize int) (*ClusterResult, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	if nPoints*dimensions != len(data) {
		return nil, errors.Newf("data length %d != nPoints %d * dimensions %d", len(data), nPoints, dimensions)
	}

	cResult := C.embedding_cluster_hdbscan(
		(*C.float)(unsafe.Pointer(&data[0])),
		C.int(nPoints),
		C.int(dimensions),
		C.int(minClusterSize),
	)

	if cResult.success == 0 {
		var errMsg string
		if cResult.error_msg != nil {
			errMsg = C.GoString(cResult.error_msg)
			C.embedding_free_string(cResult.error_msg)
		} else {
			errMsg = "HDBSCAN clustering failed"
		}
		return nil, errors.New(errMsg)
	}

	count := int(cResult.count)

	// Copy labels from C array to Go slice
	labels := make([]int32, count)
	cLabels := unsafe.Slice((*int32)(unsafe.Pointer(cResult.labels)), count)
	copy(labels, cLabels)
	C.embedding_free_int_array(cResult.labels, cResult.count)

	// Copy probabilities from C array to Go slice
	probabilities := make([]float32, count)
	cProbs := unsafe.Slice((*float32)(unsafe.Pointer(cResult.probabilities)), count)
	copy(probabilities, cProbs)
	C.embedding_free_float_array(cResult.probabilities, cResult.count)

	// Count noise points
	nNoise := 0
	for _, l := range labels {
		if l < 0 {
			nNoise++
		}
	}

	return &ClusterResult{
		Labels:        labels,
		Probabilities: probabilities,
		NClusters:     int(cResult.n_clusters),
		NPoints:       count,
		NNoise:        nNoise,
	}, nil
}

//go:build cgo && rustduckdb

package duckdbcgo

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// The ADR-024 minimum performance floor:
//   30 attestations/s written, sustained for at least 10 seconds
//   300 attestations/s read, sustained for at least 10 seconds
//   attestation size: ~1 KB
//
// This test enforces the floor in CI against a file:// location. The Lightsail
// benchmark against a real S3 bucket is a separate release-gate step (see ADR).
const (
	floorWriteRate   = 30
	floorReadRate    = 300
	floorDurationSec = 10
	targetAttestKB   = 1
)

// TestPerformanceFloor is the ADR-024 CI gate. Fails the build if either rate
// is not sustained for the required duration.
func TestPerformanceFloor(t *testing.T) {
	loc := "file://" + filepath.Join(t.TempDir(), "qntx-parquet")

	store, err := NewDuckdbStore(loc)
	if err != nil {
		t.Fatalf("NewDuckdbStore(%q) failed: %v", loc, err)
	}
	t.Cleanup(func() { store.Close() })

	// --- Write phase: floorWriteRate/s for floorDurationSec ---
	targetWrites := floorWriteRate * floorDurationSec
	writeStart := time.Now()
	ids := make([]string, 0, targetWrites)
	for i := 0; i < targetWrites; i++ {
		as := makeAttestation(fmt.Sprintf("AS-bench-w-%d", i), targetAttestKB)
		if err := store.CreateAttestation(as); err != nil {
			t.Fatalf("CreateAttestation(#%d) failed: %v", i, err)
		}
		ids = append(ids, as.ID)
	}
	writeDur := time.Since(writeStart)

	writeRate := float64(targetWrites) / writeDur.Seconds()
	t.Logf("write phase: %d attestations in %s = %.1f writes/s (floor: %d/s)",
		targetWrites, writeDur.Round(time.Millisecond), writeRate, floorWriteRate)

	if writeRate < float64(floorWriteRate) {
		t.Errorf("write throughput %.1f/s < floor %d/s", writeRate, floorWriteRate)
	}

	// --- Read phase: floorReadRate/s for floorDurationSec ---
	targetReads := floorReadRate * floorDurationSec
	readStart := time.Now()
	for i := 0; i < targetReads; i++ {
		id := ids[i%len(ids)]
		got, err := store.GetAttestation(id)
		if err != nil {
			t.Fatalf("GetAttestation(%q) at read #%d failed: %v", id, i, err)
		}
		if got == nil {
			t.Fatalf("GetAttestation(%q) = nil at read #%d", id, i)
		}
	}
	readDur := time.Since(readStart)

	readRate := float64(targetReads) / readDur.Seconds()
	t.Logf("read phase: %d attestations in %s = %.1f reads/s (floor: %d/s)",
		targetReads, readDur.Round(time.Millisecond), readRate, floorReadRate)

	if readRate < float64(floorReadRate) {
		t.Errorf("read throughput %.1f/s < floor %d/s", readRate, floorReadRate)
	}
}

// makeAttestation builds an attestation of approximately sizeKB kilobytes,
// primarily by padding the attributes map with a large string value.
func makeAttestation(id string, sizeKB int) *types.As {
	// Reserve ~200 bytes for structural fields; pad the rest via a single
	// attribute value. 1 KB target → ~800 bytes of padding.
	padBytes := sizeKB*1024 - 200
	if padBytes < 0 {
		padBytes = 0
	}
	pad := strings.Repeat("x", padBytes)

	return &types.As{
		ID:         id,
		Subjects:   []string{"BENCH"},
		Predicates: []string{"benched"},
		Contexts:   []string{"parquet-floor"},
		Actors:     []string{"human:bench"},
		Timestamp:  time.UnixMilli(1_700_000_000_000),
		Source:     "bench",
		Attributes: map[string]interface{}{"pad": pad},
		CreatedAt:  time.UnixMilli(1_700_000_000_000),
	}
}

package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/errors"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// rawInOrder is a raw store that writes into a shared order, so a test can
// say which of two stores took a write first.
type rawInOrder struct {
	name    string
	order   *[]string
	held    map[string]*types.As
	refuses error
}

func newRawInOrder(name string, order *[]string) *rawInOrder {
	return &rawInOrder{name: name, order: order, held: map[string]*types.As{}}
}

func (r *rawInOrder) CreateAttestation(as *types.As) error {
	if r.refuses != nil {
		return r.refuses
	}
	*r.order = append(*r.order, r.name)
	r.held[as.ID] = as
	return nil
}

func (r *rawInOrder) GetAttestation(id string) (*types.As, error) { return r.held[id], nil }
func (r *rawInOrder) AttestationExists(id string) bool            { _, ok := r.held[id]; return ok }
func (r *rawInOrder) CountAttestations() (int, error)             { return len(r.held), nil }
func (r *rawInOrder) GetAttestations(ats.AttestationFilter) ([]*types.As, error) {
	out := make([]*types.As, 0, len(r.held))
	for _, as := range r.held {
		out = append(out, as)
	}
	return out, nil
}

func TestAWriteLandsInTheFirstStoreThenTheRecord(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	record := newRawInOrder("S3", &order)
	pair := LandsFirst(first, record)

	require.NoError(t, pair.CreateAttestation(&types.As{ID: "AS-1"}))
	assert.Equal(t, []string{"operational db", "S3"}, order)
	assert.True(t, first.AttestationExists("AS-1"))
	assert.True(t, record.AttestationExists("AS-1"))
}

func TestARecordThatWillNotTakeTheWriteIsSaidAndTheFirstStoreKeepsIt(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	record := newRawInOrder("S3", &order)
	record.refuses = errors.New("S3 said no")
	pair := LandsFirst(first, record)

	err := pair.CreateAttestation(&types.As{ID: "AS-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AS-1 is in the operational db but its record was not written")
	assert.Contains(t, err.Error(), "S3 said no")
	assert.True(t, first.AttestationExists("AS-1"))
	assert.False(t, record.AttestationExists("AS-1"))
}

func TestAFirstStoreThatWillNotTakeTheWriteWritesNoRecord(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	first.refuses = errors.New("disk full")
	record := newRawInOrder("S3", &order)
	pair := LandsFirst(first, record)

	err := pair.CreateAttestation(&types.As{ID: "AS-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AS-1 did not land in the operational db")
	assert.Empty(t, order)
	assert.False(t, record.AttestationExists("AS-1"))
}

func TestReadsAreAnsweredByTheRecord(t *testing.T) {
	var order []string
	first := newRawInOrder("operational db", &order)
	record := newRawInOrder("S3", &order)
	record.held["AS-old"] = &types.As{ID: "AS-old"}
	pair := LandsFirst(first, record)

	assert.True(t, pair.AttestationExists("AS-old"))
	got, err := pair.GetAttestation("AS-old")
	require.NoError(t, err)
	assert.Equal(t, "AS-old", got.ID)
	n, err := pair.CountAttestations()
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	held, err := pair.(QueryableStore).GetAttestations(ats.AttestationFilter{})
	require.NoError(t, err)
	assert.Len(t, held, 1)
}

//go:build cgo && rustembeddings

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

// createObserverWithRichFields sets up an EmbeddingObserver with a real DB
// containing type definitions that declare the given rich string fields.
func createObserverWithRichFields(t *testing.T, richFields []string) *EmbeddingObserver {
	t.Helper()
	db := qntxtest.CreateTestDB(t)

	store := storage.NewSQLStore(db, zap.NewNop().Sugar())
	typeDef := &types.As{
		ID:         "ASTEST-TYPE-001",
		Subjects:   []string{"testtype"},
		Predicates: []string{"type"},
		Contexts:   []string{"graph"},
		Attributes: map[string]interface{}{
			"display_label":      "T",
			"display_color":      "#000000",
			"rich_string_fields": richFields,
		},
	}
	err := store.CreateAttestation(typeDef)
	assert.NoError(t, err)

	return &EmbeddingObserver{
		richStore: storage.NewBoundedStore(db, zap.NewNop().Sugar()),
		logger:    zap.NewNop().Sugar(),
	}
}

func TestExtractRichText_Mikolov(t *testing.T) {
	// Tomas Mikolov — Word2Vec showed that "king - man + woman = queen"
	observer := createObserverWithRichFields(t, []string{"insight"})

	as := &types.As{
		ID: "AS-MIKOLOV-001",
		Attributes: map[string]interface{}{
			"insight": "A neural network trained on raw text learns that king minus man plus woman equals queen",
		},
	}

	text := observer.extractRichText(as)
	assert.Equal(t, "A neural network trained on raw text learns that king minus man plus woman equals queen", text)
}

func TestExtractRichText_BengioCurriculum(t *testing.T) {
	// Yoshua Bengio — "A Neural Probabilistic Language Model" (2003),
	// the paper that introduced learned distributed representations for words
	observer := createObserverWithRichFields(t, []string{"contribution", "insight"})

	as := &types.As{
		ID: "AS-BENGIO-001",
		Attributes: map[string]interface{}{
			"contribution": "Proposed learning a distributed representation for words that allows the model to generalize to unseen word sequences",
			"insight":      "The curse of dimensionality is fought by learning to map each word to a continuous vector",
		},
	}

	text := observer.extractRichText(as)
	assert.Contains(t, text, "distributed representation")
	assert.Contains(t, text, "curse of dimensionality")
}

func TestExtractRichText_VaswaniAttention(t *testing.T) {
	// "Attention Is All You Need" (2017) — the transformer architecture
	// that made modern sentence embeddings possible
	observer := createObserverWithRichFields(t, []string{"abstract"})

	as := &types.As{
		ID: "AS-VASWANI-001",
		Attributes: map[string]interface{}{
			"abstract": "We propose a new simple network architecture based solely on attention mechanisms, dispensing with recurrence and convolutions entirely",
		},
	}

	text := observer.extractRichText(as)
	assert.Equal(t, "We propose a new simple network architecture based solely on attention mechanisms, dispensing with recurrence and convolutions entirely", text)
}

func TestExtractRichText_ReimersGuptaSentenceBERT(t *testing.T) {
	// Nils Reimers & Iryna Gurevych — Sentence-BERT (2019),
	// the siamese network architecture that all-MiniLM-L6-v2 descends from
	observer := createObserverWithRichFields(t, []string{"method", "result"})

	as := &types.As{
		ID: "AS-REIMERS-001",
		Attributes: map[string]interface{}{
			"method": []interface{}{
				"Siamese BERT networks derive semantically meaningful sentence embeddings that can be compared using cosine similarity",
				"Finding the most similar pair in a collection of 10000 sentences is reduced from 65 hours to about 5 seconds",
			},
		},
	}

	text := observer.extractRichText(as)
	assert.Contains(t, text, "Siamese BERT")
	assert.Contains(t, text, "5 seconds")
}

func TestExtractRichText_NoAttributes(t *testing.T) {
	observer := createObserverWithRichFields(t, []string{"insight"})

	// Firth (1957): "You shall know a word by the company it keeps"
	// — but an attestation without attributes keeps no company
	as := &types.As{ID: "AS-FIRTH-001"}
	assert.Empty(t, observer.extractRichText(as))
}

func TestExtractRichText_NoMatchingFields(t *testing.T) {
	observer := createObserverWithRichFields(t, []string{"insight"})

	// Harris (1954): distributional hypothesis — words in similar contexts
	// have similar meanings. But "unrelated_key" is not a rich field.
	as := &types.As{
		ID: "AS-HARRIS-001",
		Attributes: map[string]interface{}{
			"unrelated_key": "Words that occur in similar contexts tend to have similar meanings",
		},
	}

	assert.Empty(t, observer.extractRichText(as))
}

func TestExtractRichText_EmptyStringSkipped(t *testing.T) {
	observer := createObserverWithRichFields(t, []string{"insight"})

	// An empty embedding is like Hinton's dropout — nothing fires
	as := &types.As{
		ID:         "AS-HINTON-001",
		Attributes: map[string]interface{}{"insight": ""},
	}

	assert.Empty(t, observer.extractRichText(as))
}

func TestExtractRichText_NoRichFieldsDefined(t *testing.T) {
	// No type definitions in the DB — no rich fields to discover
	db := qntxtest.CreateTestDB(t)
	observer := &EmbeddingObserver{
		richStore: storage.NewBoundedStore(db, zap.NewNop().Sugar()),
		logger:    zap.NewNop().Sugar(),
	}

	as := &types.As{
		ID: "AS-PENNINGTON-001",
		Attributes: map[string]interface{}{
			"insight": "GloVe combines global matrix factorization with local context window methods",
		},
	}

	assert.Empty(t, observer.extractRichText(as))
}

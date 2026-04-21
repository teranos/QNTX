package embeddings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandler_NilServiceFields(t *testing.T) {
	h := &Handler{}
	assert.Nil(t, h.Store)
	assert.Nil(t, h.Service)
	assert.Nil(t, h.Logger)
}

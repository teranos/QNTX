//go:build !libacp

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

// A QNTX built without libACP runs no ACP harness, and says so where it is spoken to.
func TestAgyWithoutLibACPSaysWhy(t *testing.T) {
	s, _ := runningAgyToo(t)
	_, refused := sayingToAgy(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "libACP")
}

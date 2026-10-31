//go:build qntxquote

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
	"go.uber.org/zap/zaptest"
)

// A span in double quotes is measured against what people said where the
// caller stands, and answered as a Quote naming the turn it came from.
func TestTheTranscriptsSigilMeasuresQuotedSpansWhereTheCallerStands(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)
	at := time.Date(2026, 10, 1, 22, 0, 0, 0, time.UTC)
	for _, as := range aSession(at) {
		require.NoError(t, store.CreateAttestation(as))
	}
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), held: servingOne(db, store), logger: zaptest.NewLogger(t).Sugar()}
	signum := s.transcriptsSignum()
	require.NoError(t, signum.Check())
	asked := rootRequest(httptest.NewRequest(http.MethodGet, "/api/transcripts/quoted", nil))

	text := "the commit says \"Build QNTX here\" and \"words nobody ever typed\", and 「[Fable] not \"this\"」"
	answer, refused := signum.Answers["quoted"](sigil.WithCaller(context.Background(), asked), sigil.Sent{"text": text})
	require.Nil(t, refused)
	holds(t, signum, "quoted", answer)
	got := answer.(*protocol.Quotes).GetQuotes()
	require.Len(t, got, 2)
	assert.Equal(t, "Build QNTX here", got[0].GetContent())
	assert.Equal(t, "0", got[0].GetCert())
	assert.NotEmpty(t, got[0].GetSource(), "an exact span names the row it came from")
	assert.Equal(t, "words nobody ever typed", got[1].GetContent())
	assert.NotEqual(t, "0", got[1].GetCert())
	assert.Empty(t, got[1].GetSource(), "a span no one said names no row")

	_, refused = signum.Answers["quoted"](sigil.WithCaller(context.Background(), asked), sigil.Sent{})
	require.NotNil(t, refused)
	assert.Equal(t, "text", refused.GetParam())
}

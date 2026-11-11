package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 「[Opus 5.5] OTLP/HTTP: a 4xx answers with a google.rpc.Status, in the encoding the export came in.」
// 「[Opus 5.5] The gate refuses an export to ix/otel the same way, so a sender reads why it was turned away.」
func TestARefusedExportToIxOtelIsAGoogleRPCStatus(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	guarded := h.Middleware("/ix/otel/", Also(), func(http.ResponseWriter, *http.Request) {})

	for _, encoding := range []string{"application/x-protobuf", "application/json"} {
		asked := httptest.NewRequest(http.MethodPost, "/ix/otel/v1/traces", nil)
		asked.Header.Set("Content-Type", encoding)
		w := httptest.NewRecorder()
		guarded(w, asked)

		require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		assert.Equal(t, encoding, w.Header().Get("Content-Type"))

		said := &status.Status{}
		if encoding == "application/json" {
			require.NoError(t, protojson.Unmarshal(w.Body.Bytes(), said), w.Body.String())
		} else {
			require.NoError(t, proto.Unmarshal(w.Body.Bytes(), said), w.Body.String())
		}
		assert.Equal(t, int32(codes.Unauthenticated), said.GetCode(), encoding)
		assert.NotEmpty(t, said.GetMessage(), encoding)
	}
}

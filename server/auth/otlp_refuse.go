package auth

import (
	"mime"
	"net/http"

	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 「[Opus 5.5] ixOtelPrefix is where OpenTelemetry is taken in (server/ix_otel.go).」
const ixOtelPrefix = "/ix/otel/"

// 「[Opus 5.5] otlpCode is the google.rpc.Code of an authentication and an authorization refusal.」
var otlpCode = map[int]codes.Code{http.StatusUnauthorized: codes.Unauthenticated, http.StatusForbidden: codes.PermissionDenied}

// 「[Opus 5.5] refuseOTLP refuses an export as OTLP/HTTP requires of every 4xx: a google.rpc.Status, in the」
// 「[Opus 5.5] Content-Type the export came in, binary protobuf when it names neither of OTLP's two.」
func (h *Handler) refuseOTLP(w http.ResponseWriter, r *http.Request, code int, message string) {
	refused := &status.Status{Code: int32(otlpCode[code]), Message: message}
	encoding := "application/x-protobuf"
	if media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil && media == "application/json" {
		encoding = media
	}
	var body []byte
	var err error
	if encoding == "application/json" {
		body, err = protojson.Marshal(refused)
	} else {
		body, err = proto.Marshal(refused)
	}
	if err != nil {
		h.logger.Errorw("An ix/otel refusal did not marshal", "status", code, "path", r.URL.Path, "intended_error", message, "error", err)
		w.WriteHeader(code)
		return
	}
	w.Header().Set("Content-Type", encoding)
	w.WriteHeader(code)
	if _, err := w.Write(body); err != nil {
		h.logger.Errorw("An ix/otel refusal failed to send after its status was written",
			"status", code, "path", r.URL.Path, "intended_error", message, "error", err)
	}
}

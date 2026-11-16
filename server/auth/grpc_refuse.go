package auth

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// A gRPC call the gate refuses is answered as gRPC says a refusal: a response
// that is its trailers alone, the status in Grpc-Status and why in
// Grpc-Message (gRPC over HTTP/2, "Trailers-Only"). A2A's gRPC binding is the
// one served so (server/a2a_grpc.go).

// grpcCode is the gRPC status code a refusal's HTTP status is, as gRPC maps
// them (google.rpc.Code).
var grpcCode = map[int]int{
	http.StatusBadRequest:          3,  // INVALID_ARGUMENT
	http.StatusUnauthorized:        16, // UNAUTHENTICATED
	http.StatusForbidden:           7,  // PERMISSION_DENIED
	http.StatusNotFound:            5,  // NOT_FOUND
	http.StatusTooManyRequests:     8,  // RESOURCE_EXHAUSTED
	http.StatusServiceUnavailable:  14, // UNAVAILABLE
	http.StatusInternalServerError: 13, // INTERNAL
}

// asksOverGRPC is whether r is a gRPC call: its content type says so.
func asksOverGRPC(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc")
}

func (h *Handler) refuseGRPC(w http.ResponseWriter, status int, message string) {
	code, mapped := grpcCode[status]
	if !mapped {
		code = 2 // UNKNOWN
	}
	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("Grpc-Status", strconv.Itoa(code))
	// Grpc-Message is percent-encoded.
	w.Header().Set("Grpc-Message", url.PathEscape(message))
	w.WriteHeader(http.StatusOK)
}

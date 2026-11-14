package server

import (
	"context"
	"net/http"
	"strconv"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/teranos/QNTX/internal/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// "Harness is a plugin that speaks grpc A2A"
// "The condition is no handwritten A2A, use spec faithfully"
//
// A2A's third binding (§10): the node's A2AService over gRPC, a2a-go's
// a2agrpc over the same request handler HTTP+JSON and JSON-RPC are served
// from, so the three answer alike (§5). It is served on the node's own port,
// at the path gRPC names the service by, through the gate as every route is.

// a2aOverGRPC is the gRPC binding of handler: each version a2aSpoken holds,
// at its own service path. The path is the version, so the version a caller
// names, or names none, does not matter.
func (s *QNTXServer) a2aOverGRPC(handler a2asrv.RequestHandler) http.HandlerFunc {
	served := grpc.NewServer(grpc.MaxRecvMsgSize(nodeBodyMost))
	for _, v := range a2aSpoken {
		v.grpc(v.answeredBy(handler, s), served)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if v, spoken := spokenOverGRPC(r.URL.Path); spoken {
			r.Header.Set(a2asdk.SvcParamVersion, string(v.version))
		}
		id := uuid.NewString()
		w.Header().Set("X-Request-Id", id)
		exchange := &a2aExchange{grpc: true, path: r.URL.Path}
		r = r.WithContext(context.WithValue(logger.WithRequestID(r.Context(), id), a2aExchangeKey{}, exchange))
		if r.Body != nil {
			r.Body = &a2aBodyRead{ReadCloser: r.Body, exchange: exchange}
		}
		exchange.request = r
		served.ServeHTTP(w, r)
		// gRPC says how a call ended in its trailers.
		said, err := strconv.Atoi(w.Header().Get("Grpc-Status"))
		if err != nil || codes.Code(said) == codes.OK {
			return
		}
		status := http.StatusBadRequest
		switch codes.Code(said) {
		case codes.Internal, codes.Unknown, codes.DataLoss, codes.Unavailable:
			status = http.StatusInternalServerError
		}
		s.a2aRefused(r, a2aRefusalSaid{Operation: exchange.operation, GRPC: true, Status: status,
			Reason: codes.Code(said).String(), Message: w.Header().Get("Grpc-Message")})
	}
}

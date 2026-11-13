package server

import (
	"context"
	"net/http"
	"strconv"

	"github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/teranos/QNTX/internal/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// "「[Opus 5.5] Harness is a plugin that speaks grpc A2A」"
// "The condition is no handwritten A2A, use spec faithfully"
//
// 「[Fable 5.2] A2A's third binding (§10): the node's A2AService over gRPC, a2a-go's」
// 「[Fable 5.2] a2agrpc over the same request handler HTTP+JSON and JSON-RPC are served」
// 「[Fable 5.2] from, so the three answer alike (§5). It is served on the node's own port,」
// 「[Fable 5.2] at the path gRPC names the service by, through the gate as every route is.」

// 「[Fable 5.2] a2aGRPCPrefix is where gRPC asks for A2AService: /<package>.<service>/<method>.」
const a2aGRPCPrefix = "/lf.a2a.v1.A2AService/"

// 「[Fable 5.2] a2aOverGRPC is the gRPC binding of handler.」
func (s *QNTXServer) a2aOverGRPC(handler a2asrv.RequestHandler) http.HandlerFunc {
	served := grpc.NewServer(grpc.MaxRecvMsgSize(nodeBodyMost))
	a2agrpc.NewHandler(handler).RegisterWith(served)
	return func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		w.Header().Set("X-Request-Id", id)
		exchange := &a2aExchange{grpc: true}
		r = r.WithContext(context.WithValue(logger.WithRequestID(r.Context(), id), a2aExchangeKey{}, exchange))
		if r.Body != nil {
			r.Body = &a2aBodyRead{ReadCloser: r.Body, exchange: exchange}
		}
		exchange.request = r
		served.ServeHTTP(w, r)
		// 「[Fable 5.2] gRPC says how a call ended in its trailers.」
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

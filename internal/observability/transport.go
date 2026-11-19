package observability

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// HTTPMiddleware times every request and logs it with its request ID. It must
// run after chi's RequestID middleware. Routes are labelled by pattern
// (/hosts/{id}), never by raw path, to keep cardinality bounded.
func (m *Metrics) HTTPMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			elapsed := time.Since(started)
			if m != nil {
				m.httpDuration.WithLabelValues(route, r.Method, strconv.Itoa(status)).Observe(elapsed.Seconds())
			}
			if logger != nil {
				logger.LogAttrs(r.Context(), levelFor(status), "http request",
					slog.String("request_id", chimiddleware.GetReqID(r.Context())),
					slog.String("method", r.Method),
					slog.String("route", route),
					slog.Int("status", status),
					slog.Int("bytes", ww.BytesWritten()),
					slog.Duration("duration", elapsed),
				)
			}
		})
	}
}

func levelFor(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

type requestInfoKey struct{}

// requestInfo lets inner interceptors (authentication) report the host to the
// outer one, which logs after the call.
type requestInfo struct{ hostID int64 }

// SetHostID records the authenticated host for the request's log line.
func SetHostID(ctx context.Context, hostID int64) {
	if info, ok := ctx.Value(requestInfoKey{}).(*requestInfo); ok {
		info.hostID = hostID
	}
}

// GRPCInterceptor times every ingestion call and counts it by status code. It
// must be the outermost interceptor so authentication failures are measured
// too. Failed calls are logged with the host and code; successful ones only at
// debug level, since 1,000 hosts would otherwise write tens of lines per second.
func (m *Metrics) GRPCInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		started := time.Now()
		ri := &requestInfo{}
		resp, err := handler(context.WithValue(ctx, requestInfoKey{}, ri), req)
		code := status.Code(err)
		elapsed := time.Since(started)
		if m != nil {
			m.ingestDuration.WithLabelValues(info.FullMethod, code.String()).Observe(elapsed.Seconds())
		}
		if logger != nil {
			level := slog.LevelDebug
			switch code {
			case codes.OK:
			case codes.Internal, codes.Unavailable, codes.Unknown, codes.DataLoss:
				level = slog.LevelError
			default:
				level = slog.LevelWarn
			}
			logger.LogAttrs(ctx, level, "grpc request",
				slog.String("method", info.FullMethod),
				slog.String("code", code.String()),
				slog.Int64("host_id", ri.hostID),
				slog.Duration("duration", elapsed),
			)
		}
		return resp, err
	}
}

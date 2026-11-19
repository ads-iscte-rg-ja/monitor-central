package grpcapi

import (
	"context"
	"math/rand/v2"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"monitor-central/internal/observability"
)

// Retry delays suggested to rejected agents: random, so that 1,000 agents
// rejected in the same second do not all come back in the same second.
const (
	minRetryDelay = time.Second
	maxRetryDelay = 10 * time.Second
)

// admit bounds the ingestion requests served at once (ADR-0006). Each request
// uses at most one database connection at a time, so ingestion never takes the
// whole pool and the dashboard, sessions and jobs always find a connection.
// A request over the limit is rejected at once, before authentication touches
// the database, with RESOURCE_EXHAUSTED and RetryInfo: the agent keeps the
// records in its buffer and sends them again later, so nothing is lost.
// maxInflight <= 0 disables the limit.
func admit(maxInflight int, metrics *observability.Metrics) grpc.UnaryServerInterceptor {
	if maxInflight <= 0 {
		return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(ctx, req)
		}
	}
	slots := make(chan struct{}, maxInflight)
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		select {
		case slots <- struct{}{}:
		default:
			return nil, overloaded()
		}
		metrics.AddInflight(1)
		defer func() {
			<-slots
			metrics.AddInflight(-1)
		}()
		return handler(ctx, req)
	}
}

func overloaded() error {
	//nolint:gosec // G404: scheduling jitter, not a security value
	delay := minRetryDelay + time.Duration(rand.Int64N(int64(maxRetryDelay-minRetryDelay)+1))
	st, err := status.New(codes.ResourceExhausted, "ingestion at capacity, retry later").
		WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(delay)})
	if err != nil {
		return status.Error(codes.ResourceExhausted, "ingestion at capacity, retry later")
	}
	return st.Err()
}

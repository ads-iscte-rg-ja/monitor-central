package grpcapi

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ingestv1 "github.com/ads-iscte-rg-ja/monitor-api/gen/go/monitor/ingest/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"monitor-central/internal/models"
	"monitor-central/internal/observability"
	"monitor-central/internal/services"
)

// blockingRepo holds every write until release is closed, to keep requests in flight.
type blockingRepo struct {
	release chan struct{}
	writing chan struct{}
	auths   atomic.Int32
}

func (b *blockingRepo) GetHostByAPIKey(context.Context, string) (*models.Host, error) {
	b.auths.Add(1)
	return &models.Host{ID: 7}, nil
}
func (b *blockingRepo) CreateMachineStats(context.Context, int64, []*models.MachineStat) error {
	b.writing <- struct{}{}
	<-b.release
	return nil
}
func (b *blockingRepo) CreateErrorLogs(context.Context, int64, []*models.ErrorLog) (int, error) {
	return 0, nil
}

func TestRequestsOverTheLimitAreRejectedWithARetryDelay(t *testing.T) {
	repo := &blockingRepo{release: make(chan struct{}), writing: make(chan struct{}, 1)}
	metrics := observability.NewMetrics(nil, observability.Options{})
	listener := bufconn.Listen(1 << 20)
	server := NewServer(services.NewIngestionService(repo), log.New(io.Discard, "", 0), metrics, nil, 1)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	c := &keyedClient{ingestv1.NewIngestServiceClient(conn), "valid"}
	request := &ingestv1.IngestStatsRequest{Stats: []*ingestv1.MachineStat{stat(1)}}

	first := make(chan error, 1)
	go func() { _, err := c.IngestStats(context.Background(), request); first <- err }()
	<-repo.writing // the only slot is now taken

	_, err = c.IngestStats(context.Background(), request)
	st, _ := status.FromError(err)
	if st.Code() != codes.ResourceExhausted || len(st.Details()) != 1 {
		t.Fatalf("want RESOURCE_EXHAUSTED with RetryInfo, got %v %v", st.Code(), st.Details())
	}
	info, ok := st.Details()[0].(*errdetails.RetryInfo)
	if delay := info.GetRetryDelay().AsDuration(); !ok || delay < time.Second || delay > 10*time.Second {
		t.Fatalf("retry delay must be between 1 and 10 s, got %v", info)
	}
	if repo.auths.Load() != 1 {
		t.Fatalf("a rejected request must not reach authentication (auths=%d)", repo.auths.Load())
	}

	w := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(w.Body.String(), "monitor_ingest_inflight 1") {
		t.Fatal("the in-flight gauge must show the admitted request")
	}

	close(repo.release)
	if err := <-first; err != nil {
		t.Fatalf("the admitted request must complete: %v", err)
	}
	// The slot is free again.
	repo.release = make(chan struct{})
	close(repo.release)
	go func() { <-repo.writing }()
	if _, err := c.IngestStats(context.Background(), request); err != nil {
		t.Fatalf("after the first request finished, the next must be admitted: %v", err)
	}
}

func TestAdmissionIsOptional(t *testing.T) {
	called := false
	_, err := admit(0, nil)(context.Background(), nil, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		called = true
		return nil, nil
	})
	if err != nil || !called {
		t.Fatal("with no limit every request must pass")
	}
}

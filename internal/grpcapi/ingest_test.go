package grpcapi

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	ingestv1 "github.com/ads-iscte-rg-ja/monitor-api/gen/go/monitor/ingest/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	"monitor-central/internal/models"
	"monitor-central/internal/services"
)

type fakeRepo struct {
	authErr    error
	saveErr    error
	stats      []*models.MachineStat
	logs       []*models.ErrorLog
	duplicates int
	calls      int
}

func (f *fakeRepo) GetHostByAPIKey(_ context.Context, key string) (*models.Host, error) {
	f.calls++
	if f.authErr != nil {
		return nil, f.authErr
	}
	if key != "valid" {
		return nil, models.ErrHostNotFound
	}
	return &models.Host{ID: 7}, nil
}

func (f *fakeRepo) CreateMachineStats(_ context.Context, _ int64, stats []*models.MachineStat) error {
	f.calls++
	if f.saveErr != nil {
		return f.saveErr
	}
	for i, s := range stats {
		s.ID = int64(100 + i)
	}
	f.stats = stats
	return nil
}

func (f *fakeRepo) CreateErrorLogs(_ context.Context, _ int64, logs []*models.ErrorLog) (int, error) {
	f.calls++
	if f.saveErr != nil {
		return 0, f.saveErr
	}
	f.logs = logs
	return len(logs) - f.duplicates, nil
}

// client starts the real server (interceptors and limits included) over an
// in-memory connection and returns a client that sends the given key.
func client(t *testing.T, repo *fakeRepo, key string) ingestv1.IngestServiceClient {
	t.Helper()
	listener := bufconn.Listen(4 << 20)
	server := NewServer(services.NewIngestionService(repo), log.New(io.Discard, "", 0))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(4<<20)),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if key != "" {
		return &keyedClient{ingestv1.NewIngestServiceClient(conn), key}
	}
	return ingestv1.NewIngestServiceClient(conn)
}

type keyedClient struct {
	ingestv1.IngestServiceClient
	key string
}

func (c *keyedClient) IngestStats(ctx context.Context, in *ingestv1.IngestStatsRequest, opts ...grpc.CallOption) (*ingestv1.IngestStatsResponse, error) {
	return c.IngestServiceClient.IngestStats(metadata.AppendToOutgoingContext(ctx, ingestv1.APIKeyMetadata, c.key), in, opts...)
}

func (c *keyedClient) IngestErrors(ctx context.Context, in *ingestv1.IngestErrorsRequest, opts ...grpc.CallOption) (*ingestv1.IngestErrorsResponse, error) {
	return c.IngestServiceClient.IngestErrors(metadata.AppendToOutgoingContext(ctx, ingestv1.APIKeyMetadata, c.key), in, opts...)
}

func stat(id int64) *ingestv1.MachineStat {
	return &ingestv1.MachineStat{SourceId: id, CpuPercent: 50, RamUsedMb: 1, RamTotalMb: 2, DiskUsedGb: 1, DiskTotalGb: 2,
		CollectedAt: timestamppb.New(time.Now().Add(-time.Minute))}
}

func errorLog(id int64) *ingestv1.ErrorLog {
	return &ingestv1.ErrorLog{SourceId: id, AppName: "app", Environment: "prod", Level: ingestv1.LogLevel_LOG_LEVEL_ERROR, Message: "boom"}
}

func code(t *testing.T, err error) codes.Code {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("not a gRPC status: %v", err)
	}
	return st.Code()
}

func TestRequestsWithoutAPlausibleKeyAreRejectedBeforeReadingThePayload(t *testing.T) {
	for _, key := range []string{"", strings.Repeat("k", maxAPIKeyLength+1)} {
		repo := &fakeRepo{}
		_, err := client(t, repo, key).IngestStats(context.Background(), &ingestv1.IngestStatsRequest{Stats: []*ingestv1.MachineStat{stat(1)}})
		if code(t, err) != codes.Unauthenticated || repo.calls != 0 {
			t.Fatalf("key length %d: code=%v repo calls=%d", len(key), code(t, err), repo.calls)
		}
	}
}

func TestHostIsResolvedOnlyFromTheKey(t *testing.T) {
	repo := &fakeRepo{}
	_, err := client(t, repo, "unknown").IngestStats(context.Background(), &ingestv1.IngestStatsRequest{Stats: []*ingestv1.MachineStat{stat(1)}})
	if code(t, err) != codes.Unauthenticated || repo.stats != nil {
		t.Fatalf("unknown key: code=%v, stats persisted=%v", code(t, err), repo.stats != nil)
	}

	repo = &fakeRepo{authErr: errors.New("database down")}
	_, err = client(t, repo, "valid").IngestStats(context.Background(), &ingestv1.IngestStatsRequest{Stats: []*ingestv1.MachineStat{stat(1)}})
	if code(t, err) != codes.Unavailable {
		t.Fatalf("auth backend failure must be retryable (UNAVAILABLE), got %v", code(t, err))
	}
}

func TestIngestStatsAcknowledgesEveryRecord(t *testing.T) {
	repo := &fakeRepo{}
	resp, err := client(t, repo, "valid").IngestStats(context.Background(), &ingestv1.IngestStatsRequest{Stats: []*ingestv1.MachineStat{stat(1), stat(2)}})
	if err != nil {
		t.Fatal(err)
	}
	acks := resp.GetAcknowledgements()
	if len(acks) != 2 || acks[0].GetHostId() != 7 || acks[0].GetSourceId() != 1 || acks[1].GetSourceId() != 2 || acks[0].GetId() <= 0 {
		t.Fatalf("unexpected acknowledgements: %v", acks)
	}
	if repo.stats[0].HostID != 7 {
		t.Fatal("record not bound to the authenticated host")
	}
}

func TestInvalidRecordIsIdentifiedByAStableReason(t *testing.T) {
	future := stat(2)
	future.CollectedAt = timestamppb.New(time.Now().Add(time.Hour))
	badCPU := stat(3)
	badCPU.CpuPercent = 150
	badMetadata := errorLog(4)
	badMetadata.MetadataJson = "{"
	noLevel := errorLog(5)
	noLevel.Level = ingestv1.LogLevel_LOG_LEVEL_UNSPECIFIED

	for _, test := range []struct {
		name   string
		call   func(ingestv1.IngestServiceClient) error
		source int64
		reason ingestv1.RejectionReason
	}{
		{"future timestamp", func(c ingestv1.IngestServiceClient) error {
			_, err := c.IngestStats(context.Background(), &ingestv1.IngestStatsRequest{Stats: []*ingestv1.MachineStat{stat(1), future}})
			return err
		}, 2, ingestv1.RejectionReason_REJECTION_REASON_FUTURE_TIMESTAMP},
		{"cpu out of range", func(c ingestv1.IngestServiceClient) error {
			_, err := c.IngestStats(context.Background(), &ingestv1.IngestStatsRequest{Stats: []*ingestv1.MachineStat{badCPU}})
			return err
		}, 3, ingestv1.RejectionReason_REJECTION_REASON_INVALID_CPU_PERCENT},
		{"metadata not JSON", func(c ingestv1.IngestServiceClient) error {
			_, err := c.IngestErrors(context.Background(), &ingestv1.IngestErrorsRequest{Errors: []*ingestv1.ErrorLog{badMetadata}})
			return err
		}, 4, ingestv1.RejectionReason_REJECTION_REASON_INVALID_METADATA},
		{"level unspecified", func(c ingestv1.IngestServiceClient) error {
			_, err := c.IngestErrors(context.Background(), &ingestv1.IngestErrorsRequest{Errors: []*ingestv1.ErrorLog{noLevel}})
			return err
		}, 5, ingestv1.RejectionReason_REJECTION_REASON_INVALID_LOG_LEVEL},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &fakeRepo{}
			err := test.call(client(t, repo, "valid"))
			st, _ := status.FromError(err)
			if st.Code() != codes.InvalidArgument || len(st.Details()) != 1 {
				t.Fatalf("want INVALID_ARGUMENT with one detail, got %v %v", st.Code(), st.Details())
			}
			rejection, ok := st.Details()[0].(*ingestv1.RecordRejection)
			if !ok || rejection.GetSourceId() != test.source || rejection.GetReason() != test.reason {
				t.Fatalf("unexpected rejection %v", st.Details()[0])
			}
			if repo.stats != nil || repo.logs != nil {
				t.Fatal("a rejected batch must not be persisted")
			}
		})
	}
}

func TestMalformedBatchIsAContractViolationWithoutRecordDetail(t *testing.T) {
	for name, req := range map[string]*ingestv1.IngestStatsRequest{
		"empty":         {},
		"duplicate IDs": {Stats: []*ingestv1.MachineStat{stat(1), stat(1)}},
		"missing ID":    {Stats: []*ingestv1.MachineStat{stat(0)}},
	} {
		_, err := client(t, &fakeRepo{}, "valid").IngestStats(context.Background(), req)
		st, _ := status.FromError(err)
		if st.Code() != codes.InvalidArgument || len(st.Details()) != 0 {
			t.Fatalf("%s: want INVALID_ARGUMENT without details, got %v %v", name, st.Code(), st.Details())
		}
	}
}

func TestMessageAboveTheContractLimitIsResourceExhausted(t *testing.T) {
	huge := errorLog(1)
	huge.Message = strings.Repeat("x", ingestv1.MaxMessageBytes)
	_, err := client(t, &fakeRepo{}, "valid").IngestErrors(context.Background(), &ingestv1.IngestErrorsRequest{Errors: []*ingestv1.ErrorLog{huge}})
	st, _ := status.FromError(err)
	if st.Code() != codes.ResourceExhausted || len(st.Details()) != 0 {
		t.Fatalf("want RESOURCE_EXHAUSTED without RetryInfo, got %v %v", st.Code(), st.Details())
	}
}

func TestIngestErrorsReportsDuplicates(t *testing.T) {
	repo := &fakeRepo{duplicates: 1}
	resp, err := client(t, repo, "valid").IngestErrors(context.Background(), &ingestv1.IngestErrorsRequest{Errors: []*ingestv1.ErrorLog{errorLog(1), errorLog(2)}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetAccepted() != 2 || resp.GetInserted() != 1 || resp.GetDuplicates() != 1 || len(resp.GetSourceIds()) != 2 {
		t.Fatalf("unexpected response %v", resp)
	}
	if repo.logs[0].Level != "ERROR" || repo.logs[0].HostID != 7 {
		t.Fatalf("level or host not mapped: %+v", repo.logs[0])
	}
}

func TestStorageFailureDoesNotLeakDetails(t *testing.T) {
	repo := &fakeRepo{saveErr: errors.New("pq: password authentication failed for user monitor")}
	_, err := client(t, repo, "valid").IngestStats(context.Background(), &ingestv1.IngestStatsRequest{Stats: []*ingestv1.MachineStat{stat(1)}})
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal || strings.Contains(st.Message(), "password") {
		t.Fatalf("want a generic INTERNAL error, got %v %q", st.Code(), st.Message())
	}
}

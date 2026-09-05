package postgres_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ShanilKoshitha/goforge/job"
	jobpostgres "github.com/ShanilKoshitha/goforge/job/postgres"
)

type postgresPayload struct {
	Value string `json:"value"`
}

func TestSchemaIsPlainInspectablePostgreSQL(t *testing.T) {
	for _, fragment := range []string{
		"CREATE TABLE goforge_jobs", "CREATE TABLE goforge_failed_jobs",
		"lease_generation bigint", "goforge_jobs_active_dedup_idx",
		"available_at timestamptz", "backoff_ms bigint[]",
	} {
		if !strings.Contains(jobpostgres.Schema, fragment) {
			t.Fatalf("schema is missing %q", fragment)
		}
	}
	if strings.Contains(strings.ToLower(jobpostgres.Schema), "create extension") {
		t.Fatal("default schema unexpectedly requires an extension")
	}
}

func TestStoreConfigurationIsDefensive(t *testing.T) {
	if _, err := jobpostgres.New(nil); err == nil {
		t.Fatal("expected nil database error")
	}
	db := &sql.DB{}
	if _, err := jobpostgres.New(db, jobpostgres.WithTables("bad-name", "failed")); err == nil {
		t.Fatal("expected invalid identifier error")
	}
	if _, err := jobpostgres.New(db, jobpostgres.WithLimits(0, 1)); err == nil {
		t.Fatal("expected invalid limit error")
	}
	store, err := jobpostgres.New(db)
	if err != nil {
		t.Fatal(err)
	}
	var transaction *sql.Tx
	if _, err := store.Enqueue(context.Background(), transaction, job.EnqueueRequest{}); err == nil || !strings.Contains(err.Error(), "executor is required") {
		t.Fatalf("typed-nil executor error = %v", err)
	}
}

func TestPostgresDurableWorkflow(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := strings.ReplaceAll(time.Now().UTC().Format("150405.000000000"), ".", "")
	jobsTable := "goforge_jobs_" + suffix
	failedTable := "goforge_failed_" + suffix
	schema := strings.ReplaceAll(jobpostgres.Schema, "goforge_failed_jobs", failedTable)
	schema = strings.ReplaceAll(schema, "goforge_jobs", jobsTable)
	drop := `DROP TABLE IF EXISTS "` + failedTable + `"; DROP TABLE IF EXISTS "` + jobsTable + `";`
	if _, err := db.ExecContext(ctx, schema); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(context.Background(), drop) //nolint:errcheck

	store, err := jobpostgres.New(db, jobpostgres.WithTables(jobsTable, failedTable))
	if err != nil {
		t.Fatal(err)
	}
	definition := job.MustDefine[postgresPayload]("tests.postgres.v1", job.Policy{
		Queue: "default", MaxAttempts: 2, Timeout: time.Second, Backoff: []time.Duration{20 * time.Millisecond},
	})
	dispatcher, err := job.NewDispatcher(store, db, job.DispatcherConfig{})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("transaction visibility", func(t *testing.T) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err := definition.Dispatch(ctx, dispatcher.Using(tx), postgresPayload{Value: "rollback"})
		if err != nil {
			t.Fatal(err)
		}
		if claims := claim(t, ctx, store, 10, 100*time.Millisecond); len(claims) != 0 {
			t.Fatalf("uncommitted job became visible: %+v", claims)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if exists := jobExists(t, ctx, db, jobsTable, result.ID); exists {
			t.Fatal("rolled-back job remained durable")
		}

		tx, err = db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		result, err = definition.Dispatch(ctx, dispatcher.Using(tx), postgresPayload{Value: "commit"})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		claims := claim(t, ctx, store, 1, 100*time.Millisecond)
		if len(claims) != 1 || claims[0].ID != result.ID {
			t.Fatalf("committed job not claimed: %+v", claims)
		}
		wrong := claims[0].Lease
		wrong.Generation++
		if owned, err := store.Ack(ctx, wrong); err != nil || owned {
			t.Fatalf("stale fence acknowledged: owned=%v err=%v", owned, err)
		}
		if owned, err := store.Heartbeat(ctx, claims[0].Lease, 100*time.Millisecond); err != nil || !owned {
			t.Fatalf("live heartbeat failed: owned=%v err=%v", owned, err)
		}
		if owned, err := store.Ack(ctx, claims[0].Lease); err != nil || !owned {
			t.Fatalf("live ack failed: owned=%v err=%v", owned, err)
		}
	})

	t.Run("reclaim fences every stale owner mutation", func(t *testing.T) {
		if _, err := definition.Dispatch(ctx, dispatcher, postgresPayload{Value: "reclaim"}); err != nil {
			t.Fatal(err)
		}
		first, err := store.Claim(ctx, job.ClaimRequest{
			Queues: []string{"default"}, Names: []string{definition.Name()}, Limit: 1,
			WorkerID: "worker-a", LeaseDuration: 20 * time.Millisecond,
		})
		if err != nil || len(first) != 1 {
			t.Fatalf("first claim: %+v %v", first, err)
		}
		second := eventuallyClaim(t, ctx, store, job.ClaimRequest{
			Queues: []string{"default"}, Names: []string{definition.Name()}, Limit: 1,
			WorkerID: "worker-b", LeaseDuration: 200 * time.Millisecond,
		}, 500*time.Millisecond)
		if second.Lease.Generation <= first[0].Lease.Generation {
			t.Fatalf("lease generation did not advance: %d -> %d", first[0].Lease.Generation, second.Lease.Generation)
		}
		if owned, err := store.Heartbeat(ctx, first[0].Lease, time.Second); err != nil || owned {
			t.Fatalf("stale heartbeat: owned=%v err=%v", owned, err)
		}
		if owned, err := store.Ack(ctx, first[0].Lease); err != nil || owned {
			t.Fatalf("stale ack: owned=%v err=%v", owned, err)
		}
		if owned, err := store.Retry(ctx, job.RetryRequest{Lease: first[0].Lease, Delay: 0, ErrorKind: "error", Error: "stale"}); err != nil || owned {
			t.Fatalf("stale retry: owned=%v err=%v", owned, err)
		}
		if owned, err := store.Release(ctx, first[0].Lease); err != nil || owned {
			t.Fatalf("stale release: owned=%v err=%v", owned, err)
		}
		if owned, err := store.Fail(ctx, job.FailureRequest{Lease: first[0].Lease, Kind: "error", Error: "stale"}); err != nil || owned {
			t.Fatalf("stale fail: owned=%v err=%v", owned, err)
		}
		if owned, err := store.Ack(ctx, second.Lease); err != nil || !owned {
			t.Fatalf("new owner ack: owned=%v err=%v", owned, err)
		}
	})

	t.Run("concurrent active deduplication", func(t *testing.T) {
		const count = 16
		results := make(chan job.DispatchResult, count)
		errors := make(chan error, count)
		var wait sync.WaitGroup
		for index := 0; index < count; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				result, err := definition.Dispatch(ctx, dispatcher, postgresPayload{Value: "same"}, job.Deduplicate("same-key"))
				if err != nil {
					errors <- err
					return
				}
				results <- result
			}()
		}
		wait.Wait()
		close(results)
		close(errors)
		for err := range errors {
			t.Fatal(err)
		}
		var id job.ID
		enqueued := 0
		for result := range results {
			if id == "" {
				id = result.ID
			}
			if result.ID != id {
				t.Fatalf("dedup returned different IDs: %s and %s", id, result.ID)
			}
			if result.Enqueued {
				enqueued++
			}
		}
		if enqueued != 1 {
			t.Fatalf("expected one insertion, got %d", enqueued)
		}
		claims := claim(t, ctx, store, 1, 100*time.Millisecond)
		if len(claims) != 1 {
			t.Fatalf("expected deduplicated job: %+v", claims)
		}
		if owned, err := store.Ack(ctx, claims[0].Lease); err != nil || !owned {
			t.Fatalf("ack deduplicated job: %v %v", owned, err)
		}
		reused, err := definition.Dispatch(ctx, dispatcher, postgresPayload{Value: "same"}, job.Deduplicate("same-key"))
		if err != nil || !reused.Enqueued || reused.ID == id {
			t.Fatalf("completed active key was not reusable: %+v %v", reused, err)
		}
		reusedClaims := claim(t, ctx, store, 1, 100*time.Millisecond)
		if len(reusedClaims) != 1 || reusedClaims[0].ID != reused.ID {
			t.Fatalf("reused key claim = %+v", reusedClaims)
		}
		if owned, err := store.Ack(ctx, reusedClaims[0].Lease); err != nil || !owned {
			t.Fatalf("ack reused key: %v %v", owned, err)
		}
	})

	t.Run("one-off delay and unknown versions", func(t *testing.T) {
		delayed, err := definition.Dispatch(ctx, dispatcher, postgresPayload{Value: "delayed"}, job.Delay(200*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		unknown := job.MustDefine[postgresPayload]("tests.newer.v1", job.Policy{})
		if _, err := unknown.Dispatch(ctx, dispatcher, postgresPayload{Value: "unknown"}); err != nil {
			t.Fatal(err)
		}
		if claims := claim(t, ctx, store, 10, 100*time.Millisecond); len(claims) != 0 {
			t.Fatalf("delayed or unknown job was claimed early: %+v", claims)
		}
		claimed := eventuallyClaim(t, ctx, store, job.ClaimRequest{
			Queues: []string{"default"}, Names: []string{definition.Name()}, Limit: 1,
			WorkerID: "integration", LeaseDuration: 100 * time.Millisecond,
		}, 2*time.Second)
		if claimed.ID != delayed.ID {
			t.Fatalf("delayed claim = %s, want %s", claimed.ID, delayed.ID)
		}
		if owned, err := store.Ack(ctx, claimed.Lease); err != nil || !owned {
			t.Fatalf("ack delayed job: %v %v", owned, err)
		}
		unknownClaims, err := store.Claim(ctx, job.ClaimRequest{
			Queues: []string{"default"}, Names: []string{unknown.Name()}, Limit: 1,
			WorkerID: "new-worker", LeaseDuration: 100 * time.Millisecond,
		})
		if err != nil || len(unknownClaims) != 1 {
			t.Fatalf("compatible worker did not retain unknown job: %+v %v", unknownClaims, err)
		}
		if owned, err := store.Ack(ctx, unknownClaims[0].Lease); err != nil || !owned {
			t.Fatalf("ack newer job: %v %v", owned, err)
		}
	})

	t.Run("retry delay and terminal administration", func(t *testing.T) {
		if _, err := definition.Dispatch(ctx, dispatcher, postgresPayload{Value: "retry"}); err != nil {
			t.Fatal(err)
		}
		claims := claim(t, ctx, store, 1, 100*time.Millisecond)
		if len(claims) != 1 {
			t.Fatalf("expected claim: %+v", claims)
		}
		if owned, err := store.Retry(ctx, job.RetryRequest{Lease: claims[0].Lease, Delay: 40 * time.Millisecond, ErrorKind: "error", Error: "try again"}); err != nil || !owned {
			t.Fatalf("retry: owned=%v err=%v", owned, err)
		}
		if claims := claim(t, ctx, store, 1, 100*time.Millisecond); len(claims) != 0 {
			t.Fatal("delayed retry was immediately visible")
		}
		retried := eventuallyClaim(t, ctx, store, job.ClaimRequest{
			Queues: []string{"default"}, Names: []string{definition.Name()}, Limit: 1,
			WorkerID: "integration", LeaseDuration: 100 * time.Millisecond,
		}, time.Second)
		claims = []job.Delivery{retried}
		if claims[0].Attempt != 2 {
			t.Fatalf("retry attempt was not durable: %+v", claims)
		}
		if owned, err := store.Fail(ctx, job.FailureRequest{Lease: claims[0].Lease, Kind: "permanent", Error: "finished"}); err != nil || !owned {
			t.Fatalf("fail: owned=%v err=%v", owned, err)
		}
		failed, err := store.ListFailed(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(failed) != 1 || failed[0].FailureKind != "permanent" || failed[0].Attempts != 2 {
			t.Fatalf("unexpected failed job: %+v", failed)
		}
		detail, found, err := store.FindFailed(ctx, failed[0].ID)
		if err != nil || !found || !bytes.Contains(detail.Payload, []byte(`"retry"`)) {
			t.Fatalf("explicit failed payload lookup = %+v found=%v err=%v", detail, found, err)
		}
		if restored, err := store.RetryFailed(ctx, failed[0].ID); err != nil || !restored {
			t.Fatalf("restore failed job: %v %v", restored, err)
		}
		claims = claim(t, ctx, store, 1, 100*time.Millisecond)
		if len(claims) != 1 || claims[0].Attempt != 1 {
			t.Fatalf("restored job did not reset attempts: %+v", claims)
		}
		if owned, err := store.Fail(ctx, job.FailureRequest{Lease: claims[0].Lease, Kind: "forgotten", Error: "remove"}); err != nil || !owned {
			t.Fatalf("fail restored job: %v %v", owned, err)
		}
		if forgotten, err := store.ForgetFailed(ctx, claims[0].ID); err != nil || !forgotten {
			t.Fatalf("forget failed job: %v %v", forgotten, err)
		}
	})

	t.Run("exhausted crashed lease", func(t *testing.T) {
		oneAttempt := job.MustDefine[postgresPayload]("tests.once.v1", job.Policy{MaxAttempts: 1, Timeout: time.Second, Backoff: []time.Duration{time.Millisecond}})
		if _, err := oneAttempt.Dispatch(ctx, dispatcher, postgresPayload{Value: "crash"}); err != nil {
			t.Fatal(err)
		}
		claims, err := store.Claim(ctx, job.ClaimRequest{Queues: []string{"default"}, Names: []string{oneAttempt.Name()}, Limit: 1, WorkerID: "crashed", LeaseDuration: 15 * time.Millisecond})
		if err != nil || len(claims) != 1 {
			t.Fatalf("claim final attempt: %+v %v", claims, err)
		}
		deadline := time.Now().Add(500 * time.Millisecond)
		var exhausted []job.FailedJob
		for len(exhausted) == 0 && time.Now().Before(deadline) {
			exhausted, err = store.FailExhausted(ctx, []string{"default"}, []string{oneAttempt.Name()}, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(exhausted) == 0 {
				time.Sleep(5 * time.Millisecond)
			}
		}
		if len(exhausted) != 1 {
			t.Fatalf("fail exhausted: count=%d", len(exhausted))
		}
		failed, err := store.ListFailed(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range failed {
			if item.Name == oneAttempt.Name() && item.FailureKind == "lease_exhausted" {
				found = true
			}
		}
		if !found {
			t.Fatalf("exhausted failure was not recorded: %+v", failed)
		}
	})
}

func claim(t *testing.T, ctx context.Context, store *jobpostgres.Store, limit int, lease time.Duration) []job.Delivery {
	t.Helper()
	deliveries, err := store.Claim(ctx, job.ClaimRequest{
		Queues: []string{"default"}, Names: []string{"tests.postgres.v1"}, Limit: limit,
		WorkerID: "integration", LeaseDuration: lease,
	})
	if err != nil {
		t.Fatal(err)
	}
	return deliveries
}

func jobExists(t *testing.T, ctx context.Context, db *sql.DB, table string, id job.ID) bool {
	t.Helper()
	var exists bool
	query := `SELECT EXISTS (SELECT 1 FROM "` + table + `" WHERE id = $1::uuid)`
	if err := db.QueryRowContext(ctx, query, string(id)).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func eventuallyClaim(t *testing.T, ctx context.Context, store *jobpostgres.Store, request job.ClaimRequest, within time.Duration) job.Delivery {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		deliveries, err := store.Claim(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if len(deliveries) == 1 {
			return deliveries[0]
		}
		if len(deliveries) > 1 {
			t.Fatalf("claim exceeded requested one delivery: %+v", deliveries)
		}
		if time.Now().After(deadline) {
			t.Fatalf("job was not claimable within %s", within)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

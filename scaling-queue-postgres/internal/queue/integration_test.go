package queue_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

func TestStagesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("PGQUEUE_TEST_DSN")
	if dsn == "" {
		t.Skip("set PGQUEUE_TEST_DSN to run integration tests")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	store, err := queue.Open(ctx, dsn, 8, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, st := range queue.Stages() {
		t.Run(st.Name, func(t *testing.T) {
			const total = 100
			shards := 1
			if st.Sharded {
				shards = 4
			}
			if err := store.SetupStage(ctx, st); err != nil {
				t.Fatal(err)
			}
			if err := store.Enqueue(ctx, "itest", total, shards); err != nil {
				t.Fatal(err)
			}
			if n, err := store.Backlog(ctx, "itest"); err != nil || n != total {
				t.Fatalf("Backlog = %d, %v, want %d", n, err, total)
			}

			claimed := make(map[int64]int)
			var mu sync.Mutex
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				shard := i % shards
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						var ids []int64
						for {
							var err error
							ids, err = store.DequeueBatch(ctx, st, "itest", shard, 7)
							if err == nil {
								break
							}
							if !queue.Retryable(err) {
								t.Errorf("dequeue: %v", err)
								return
							}
						}
						if len(ids) == 0 {
							return
						}
						if err := store.Complete(ctx, ids); err != nil {
							t.Errorf("complete: %v", err)
							return
						}
						mu.Lock()
						for _, id := range ids {
							claimed[id]++
						}
						mu.Unlock()
					}
				}()
			}
			wg.Wait()

			if len(claimed) != total {
				t.Errorf("claimed %d distinct tasks, want %d", len(claimed), total)
			}
			for id, n := range claimed {
				if n != 1 {
					t.Errorf("task %d claimed %d times", id, n)
				}
			}
			if n, err := store.Backlog(ctx, "itest"); err != nil || n != 0 {
				t.Errorf("Backlog after drain = %d, %v, want 0", n, err)
			}
		})
	}
}

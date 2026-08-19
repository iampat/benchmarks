package queue_test

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

func TestStages(t *testing.T) {
	stages := queue.Stages()
	if len(stages) != 7 {
		t.Fatalf("got %d stages, want 7", len(stages))
	}

	seen := map[string]bool{}
	base := strings.TrimSuffix(stages[0].SelectSQL(), stages[0].LockClause)
	for _, st := range stages {
		if seen[st.Name] {
			t.Errorf("duplicate stage name %q", st.Name)
		}
		seen[st.Name] = true

		got := strings.TrimSuffix(st.SelectSQL(), st.LockClause)
		if st.Sharded {
			if !strings.Contains(got, "shard = $2") || !strings.Contains(got, "LIMIT $3") {
				t.Errorf("stage %s: sharded query missing shard predicate: %q", st.Name, got)
			}
		} else if got != base {
			t.Errorf("stage %s: query base differs from stage 0: %q", st.Name, got)
		}
		if !strings.Contains(st.IndexDDL, "tasks_dequeue_idx") {
			t.Errorf("stage %s: IndexDDL does not create tasks_dequeue_idx: %q", st.Name, st.IndexDDL)
		}
	}

	for i, st := range stages[:2] {
		if st.Iso != pgx.RepeatableRead {
			t.Errorf("stage %d iso = %v, want RepeatableRead", i, st.Iso)
		}
	}
	for i, st := range stages[2:] {
		if st.Iso != pgx.ReadCommitted {
			t.Errorf("stage %d iso = %v, want ReadCommitted", i+2, st.Iso)
		}
	}
	if stages[0].LockClause != "FOR UPDATE" {
		t.Errorf("stage 0 lock clause = %q", stages[0].LockClause)
	}
	for _, st := range stages[1:] {
		if st.LockClause != "FOR UPDATE SKIP LOCKED" {
			t.Errorf("stage %s lock clause = %q", st.Name, st.LockClause)
		}
	}

	if stages[0].IndexDDL != stages[1].IndexDDL || stages[1].IndexDDL != stages[2].IndexDDL {
		t.Error("stages 0-2 must share the same index DDL")
	}
	for _, st := range stages[3:] {
		if !strings.Contains(st.IndexDDL, "WHERE status = 'ENQUEUED'") {
			t.Errorf("stage %s index is not partial: %q", st.Name, st.IndexDDL)
		}
	}

	// Wave 2 flags accumulate one per stage.
	wantFlags := []struct {
		name            string
		syncOff         bool
		singleStatement bool
		sharded         bool
	}{
		{"3-partial-index", false, false, false},
		{"4-async-commit", true, false, false},
		{"5-single-statement", true, true, false},
		{"6-sharded", true, true, true},
	}
	for i, want := range wantFlags {
		st := stages[i+3]
		if st.Name != want.name {
			t.Errorf("stage %d name = %q, want %q", i+3, st.Name, want.name)
		}
		if (st.SyncCommit == "off") != want.syncOff ||
			st.SingleStatement != want.singleStatement || st.Sharded != want.sharded {
			t.Errorf("stage %s flags = %+v, want %+v", st.Name, st, want)
		}
	}
	if !strings.Contains(stages[6].IndexDDL, "(queue_name, shard, priority, created_at)") {
		t.Errorf("sharded index must lead with shard after queue_name: %q", stages[6].IndexDDL)
	}
}

func TestDequeueSQL(t *testing.T) {
	pending, _ := queue.StageByName("5-single-statement")
	sql := pending.DequeueSQL()
	for _, want := range []string{"WITH c AS (", "FOR UPDATE SKIP LOCKED", "status = 'PENDING'", "RETURNING id"} {
		if !strings.Contains(sql, want) {
			t.Errorf("5-single-statement DequeueSQL missing %q: %s", want, sql)
		}
	}
	if strings.Contains(sql, "completed_at") {
		t.Errorf("5-single-statement DequeueSQL must not complete: %s", sql)
	}
}

func TestStageByName(t *testing.T) {
	if _, ok := queue.StageByName("nope"); ok {
		t.Error("StageByName(nope) = ok, want miss")
	}
	st, ok := queue.StageByName("2-read-committed")
	if !ok || st.Iso != pgx.ReadCommitted {
		t.Errorf("StageByName(2-read-committed) = %+v, %v", st, ok)
	}
}

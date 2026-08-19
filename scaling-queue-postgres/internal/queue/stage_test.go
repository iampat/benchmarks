package queue_test

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

func TestStages(t *testing.T) {
	stages := queue.Stages()
	if len(stages) != 8 {
		t.Fatalf("got %d stages, want 8", len(stages))
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
		if !strings.Contains(got, "status = 'CREATED'") {
			t.Errorf("stage %s: claim query does not select CREATED rows: %q", st.Name, got)
		}
		if !strings.Contains(st.IndexDDL, "tasks_claim_idx") {
			t.Errorf("stage %s: IndexDDL does not create tasks_claim_idx: %q", st.Name, st.IndexDDL)
		}
		if st.CompletionBatch < 1 {
			t.Errorf("stage %s: completion batch %d must be at least 1", st.Name, st.CompletionBatch)
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
		if !strings.Contains(st.IndexDDL, "WHERE status = 'CREATED'") {
			t.Errorf("stage %s index is not partial: %q", st.Name, st.IndexDDL)
		}
	}

	// Each stage keeps every change of the stage above it.
	wantFlags := []struct {
		name            string
		syncOff         bool
		singleStatement bool
		sharded         bool
		completionBatch int
	}{
		{"3-partial-index", false, false, false, 1},
		{"4-async-commit", true, false, false, 1},
		{"5-single-statement", true, true, false, 1},
		{"6-sharded", true, true, true, 1},
		{"7-batched-completion", true, true, true, 100},
	}
	for i, want := range wantFlags {
		st := stages[i+3]
		if st.Name != want.name {
			t.Errorf("stage %d name = %q, want %q", i+3, st.Name, want.name)
		}
		if (st.SyncCommit == "off") != want.syncOff ||
			st.SingleStatement != want.singleStatement ||
			st.Sharded != want.sharded ||
			st.CompletionBatch != want.completionBatch {
			t.Errorf("stage %s flags = %+v, want %+v", st.Name, st, want)
		}
	}
	if !strings.Contains(stages[6].IndexDDL, "(queue_name, shard, priority, created_at)") {
		t.Errorf("sharded index must lead with shard after queue_name: %q", stages[6].IndexDDL)
	}
}

func TestClaimSQL(t *testing.T) {
	st, _ := queue.StageByName("5-single-statement")
	sql := st.ClaimSQL()
	for _, want := range []string{
		"WITH c AS (", "FOR UPDATE SKIP LOCKED",
		"status = 'PENDING'", "started_at = now()", "RETURNING id",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("ClaimSQL missing %q: %s", want, sql)
		}
	}
	if strings.Contains(sql, "completed_at") || strings.Contains(sql, "'DONE'") {
		t.Errorf("a claim must not complete the task: %s", sql)
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
	if len(queue.StageNames()) != len(queue.Stages()) {
		t.Error("StageNames must name every stage")
	}
}

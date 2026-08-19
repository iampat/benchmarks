package queue

import "github.com/jackc/pgx/v5"

type Stage struct {
	Name       string
	Iso        pgx.TxIsoLevel
	LockClause string
	IndexDDL   string
	// "" keeps the server default. Wave-2 stages set "off".
	SyncCommit string
	// One CTE statement instead of SELECT then UPDATE in a transaction.
	SingleStatement bool
	// Dequeue filters on a shard column. N shards give N queue heads.
	Sharded bool
}

// CONSIDER(ali): every stage orders by (priority, created_at) so the stage-3
// index can supply the sort. The blog's baseline orders by created_at only.
const (
	selectBase        = "SELECT id FROM tasks WHERE queue_name = $1 AND status = 'ENQUEUED' ORDER BY priority, created_at LIMIT $2 "
	selectShardedBase = "SELECT id FROM tasks WHERE queue_name = $1 AND shard = $2 AND status = 'ENQUEUED' ORDER BY priority, created_at LIMIT $3 "
)

func (s Stage) base() string {
	if s.Sharded {
		return selectShardedBase
	}
	return selectBase
}

func (s Stage) SelectSQL() string {
	return s.base() + s.LockClause
}

func (s Stage) DequeueSQL() string {
	return "WITH c AS (" + s.base() + s.LockClause + ") " +
		"UPDATE tasks SET status = 'PENDING', started_at = now() " +
		"WHERE id IN (SELECT id FROM c) RETURNING id"
}

const (
	basicIndexDDL = "CREATE INDEX tasks_dequeue_idx ON tasks (queue_name, created_at)"
	// CONSIDER(ali): (queue_name, status, created_at) is an alternative vanilla
	// index. It would shrink the stage-3 delta to "partial + presorted" only.
	partialIndexDDL = "CREATE INDEX tasks_dequeue_idx ON tasks (queue_name, status, priority, created_at) WHERE status = 'ENQUEUED'"
	shardedIndexDDL = "CREATE INDEX tasks_dequeue_idx ON tasks (queue_name, shard, priority, created_at) WHERE status = 'ENQUEUED'"
)

// Stages 0-3 replicate the DBOS article. Stages 4-6 chase a higher rate:
// 4 relaxes commit durability, 5 removes a round trip, 6 splits the queue
// head into N shards. Locking, the PENDING state, and table logging stay
// in every stage.
func Stages() []Stage {
	skip := "FOR UPDATE SKIP LOCKED"
	return []Stage{
		{Name: "0-vanilla", Iso: pgx.RepeatableRead, LockClause: "FOR UPDATE", IndexDDL: basicIndexDDL},
		{Name: "1-skip-locked", Iso: pgx.RepeatableRead, LockClause: skip, IndexDDL: basicIndexDDL},
		{Name: "2-read-committed", Iso: pgx.ReadCommitted, LockClause: skip, IndexDDL: basicIndexDDL},
		{Name: "3-partial-index", Iso: pgx.ReadCommitted, LockClause: skip, IndexDDL: partialIndexDDL},
		{
			Name: "4-async-commit", Iso: pgx.ReadCommitted, LockClause: skip, IndexDDL: partialIndexDDL,
			SyncCommit: "off",
		},
		{
			Name: "5-single-statement", Iso: pgx.ReadCommitted, LockClause: skip, IndexDDL: partialIndexDDL,
			SyncCommit: "off", SingleStatement: true,
		},
		{
			Name: "6-sharded", Iso: pgx.ReadCommitted, LockClause: skip, IndexDDL: shardedIndexDDL,
			SyncCommit: "off", SingleStatement: true, Sharded: true,
		},
	}
}

func StageByName(name string) (Stage, bool) {
	for _, st := range Stages() {
		if st.Name == name {
			return st, true
		}
	}
	return Stage{}, false
}

package queue

import "github.com/jackc/pgx/v5"

type Stage struct {
	Name       string
	Iso        pgx.TxIsoLevel
	LockClause string
	IndexDDL   string
	// "" keeps the server default. Later stages set "off".
	SyncCommit string
	// One CTE statement instead of SELECT then UPDATE in a transaction.
	SingleStatement bool
	// A shard column splits the queue head into independent index tails.
	Sharded bool
}

// CONSIDER(ali): every stage orders by (priority, created_at) so the sharded
// and partial indexes can supply the sort without a sort node.
const (
	selectBase        = "SELECT id FROM tasks WHERE queue_name = $1 AND status = 'CREATED' ORDER BY priority, created_at LIMIT $2 "
	selectShardedBase = "SELECT id FROM tasks WHERE queue_name = $1 AND shard = $2 AND status = 'CREATED' ORDER BY priority, created_at LIMIT $3 "
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

func (s Stage) ClaimSQL() string {
	return "WITH c AS (" + s.base() + s.LockClause + ") " +
		"UPDATE tasks SET status = 'PENDING', started_at = now() " +
		"WHERE id IN (SELECT id FROM c) RETURNING id"
}

const (
	basicIndexDDL = "CREATE INDEX tasks_claim_idx ON tasks (queue_name, created_at)"
	// CONSIDER(ali): (queue_name, status, created_at) is an alternative vanilla
	// index. It would shrink the partial-index delta to "partial plus presorted".
	partialIndexDDL = "CREATE INDEX tasks_claim_idx ON tasks (queue_name, status, priority, created_at) WHERE status = 'CREATED'"
	shardedIndexDDL = "CREATE INDEX tasks_claim_idx ON tasks (queue_name, shard, priority, created_at) WHERE status = 'CREATED'"
)

// Stages 0 to 3 replicate the DBOS article. Stages 4 to 6 go past it. Each
// stage keeps every change of the stage above it.
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

func StageNames() []string {
	var names []string
	for _, s := range Stages() {
		names = append(names, s.Name)
	}
	return names
}

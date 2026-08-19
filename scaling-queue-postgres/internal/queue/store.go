package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaDDL = `CREATE TABLE tasks (
	id bigserial PRIMARY KEY,
	queue_name text NOT NULL,
	shard smallint NOT NULL DEFAULT 0,
	status text NOT NULL,
	priority int NOT NULL DEFAULT 0,
	created_at timestamptz NOT NULL DEFAULT now(),
	started_at timestamptz,
	completed_at timestamptz,
	payload text NOT NULL
)`

var payload = strings.Repeat("x", 100)

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, dsn string, maxConns int32, runtimeParams map[string]string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = maxConns
	cfg.MinConns = maxConns
	for k, v := range runtimeParams {
		cfg.ConnConfig.RuntimeParams[k] = v
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) SetupStage(ctx context.Context, st Stage) error {
	stmts := []string{"DROP TABLE IF EXISTS tasks", schemaDDL, st.IndexDDL}
	for _, stmt := range stmts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("setup stage %s: %w", st.Name, err)
		}
	}
	return nil
}

func (s *Store) Checkpoint(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, "CHECKPOINT")
	return err
}

func (s *Store) Create(ctx context.Context, queueName string, n, shards int) error {
	_, err := s.pool.Exec(ctx,
		"INSERT INTO tasks (queue_name, shard, status, priority, payload) "+
			"SELECT $1, floor(random() * $4)::smallint, 'CREATED', 0, $2 FROM generate_series(1, $3)",
		queueName, payload, n, shards)
	return err
}

func (s *Store) Claim(ctx context.Context, st Stage, queueName string, shard, limit int) ([]int64, error) {
	args := []any{queueName, limit}
	if st.Sharded {
		args = []any{queueName, shard, limit}
	}
	if st.SingleStatement {
		rows, err := s.pool.Query(ctx, st.ClaimSQL(), args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[int64])
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: st.Iso})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, st.SelectSQL(), args...)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx,
		"UPDATE tasks SET status = 'PENDING', started_at = now() WHERE id = ANY($1)", ids); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *Store) Done(ctx context.Context, ids []int64) error {
	_, err := s.pool.Exec(ctx,
		"UPDATE tasks SET status = 'DONE', completed_at = now() WHERE id = ANY($1)", ids)
	return err
}

func (s *Store) Backlog(ctx context.Context, queueName string) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx,
		"SELECT count(*) FROM tasks WHERE queue_name = $1 AND status = 'CREATED'", queueName).Scan(&n)
	return n, err
}

func (s *Store) ServerInfo(ctx context.Context, settings ...string) (map[string]string, error) {
	info := make(map[string]string, len(settings)+1)
	var version string
	if err := s.pool.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		return nil, err
	}
	info["version"] = version
	for _, name := range settings {
		var v string
		if err := s.pool.QueryRow(ctx, "SELECT current_setting($1)", name).Scan(&v); err != nil {
			return nil, err
		}
		info[name] = v
	}
	return info, nil
}

func Retryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "40001" || pgErr.Code == "40P01"
}

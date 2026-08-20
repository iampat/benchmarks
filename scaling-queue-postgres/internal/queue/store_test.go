package queue_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/queue"
)

func TestRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain", errors.New("boom"), false},
		{"serialization", &pgconn.PgError{Code: "40001"}, true},
		{"deadlock", &pgconn.PgError{Code: "40P01"}, true},
		{"wrapped", fmt.Errorf("dequeue: %w", &pgconn.PgError{Code: "40001"}), true},
		{"other pg error", &pgconn.PgError{Code: "23505"}, false},
	}
	for _, tt := range tests {
		if got := queue.Retryable(tt.err); got != tt.want {
			t.Errorf("%s: Retryable = %v, want %v", tt.name, got, tt.want)
		}
	}
}

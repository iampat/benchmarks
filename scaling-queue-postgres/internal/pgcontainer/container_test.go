package pgcontainer_test

import (
	"slices"
	"testing"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/pgcontainer"
)

func TestRunArgs(t *testing.T) {
	cfg := pgcontainer.Config{
		Image:          "docker.io/library/postgres:18",
		Name:           "scaling-queue-postgres-bench",
		Port:           55432,
		Password:       "secret",
		MaxConnections: 200,
	}
	want := []string{
		"run", "-d", "--rm",
		"--name", "scaling-queue-postgres-bench",
		"-p", "127.0.0.1:55432:5432",
		"-e", "POSTGRES_PASSWORD=secret",
		"docker.io/library/postgres:18",
		"-c", "max_connections=200",
	}
	if got := cfg.RunArgs(); !slices.Equal(got, want) {
		t.Errorf("RunArgs = %q, want %q", got, want)
	}
}

func TestDSN(t *testing.T) {
	cfg := pgcontainer.Config{Port: 55432, Password: "secret"}
	want := "postgres://postgres:secret@127.0.0.1:55432/postgres"
	if got := cfg.DSN(); got != want {
		t.Errorf("DSN = %q, want %q", got, want)
	}
}

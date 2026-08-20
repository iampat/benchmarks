package pgcontainer

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Config struct {
	Image          string
	Name           string
	Port           int
	Password       string
	MaxConnections int
}

func (c Config) RunArgs() []string {
	// The image is pulled once, before the run. Never consulting a registry
	// keeps a credential helper out of the loop, which otherwise fails a long
	// run when its token expires.
	return []string{
		"run", "-d", "--rm", "--pull=never",
		"--name", c.Name,
		"-p", fmt.Sprintf("127.0.0.1:%d:5432", c.Port),
		"-e", "POSTGRES_PASSWORD=" + c.Password,
		c.Image,
		"-c", fmt.Sprintf("max_connections=%d", c.MaxConnections),
	}
}

func (c Config) DSN() string {
	return fmt.Sprintf("postgres://postgres:%s@127.0.0.1:%d/postgres", c.Password, c.Port)
}

type Container struct {
	Config Config
	ID     string
}

func Start(ctx context.Context, cfg Config) (*Container, error) {
	out, err := podman(ctx, cfg.RunArgs()...)
	if err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}
	return &Container{Config: cfg, ID: out}, nil
}

func (c *Container) Stop(ctx context.Context) error {
	_, err := podman(ctx, "stop", c.ID)
	return err
}

func (c *Container) ImageDigest(ctx context.Context) (string, error) {
	return podman(ctx, "image", "inspect", "--format", "{{.Digest}}", c.Config.Image)
}

// Returns 0 where podman runs without a machine, for example on Linux.
func MachineCPUs(ctx context.Context) int {
	out, err := podman(ctx, "machine", "inspect", "--format", "{{.Resources.CPUs}}")
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return 0
	}
	return n
}

func podman(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "podman", args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("podman %s: %w: %s", args[0], err, exitErr.Stderr)
		}
		return "", fmt.Errorf("podman %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// During initdb Postgres listens only on the unix socket, so the first
// successful TCP connect is the final server, not the bootstrap one.
func WaitReady(ctx context.Context, dsn string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("postgres not ready: %w (last error: %v)", ctx.Err(), lastErr)
			}
			return ctx.Err()
		case <-ticker.C:
			if err := ping(ctx, dsn); err != nil {
				lastErr = err
				continue
			}
			return nil
		}
	}
}

func ping(ctx context.Context, dsn string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	var one int
	return conn.QueryRow(ctx, "SELECT 1").Scan(&one)
}

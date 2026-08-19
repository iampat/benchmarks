package benchreport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Result struct {
	Benchmark        string      `json:"benchmark"`
	Stage            string      `json:"stage"`
	Workers          int         `json:"workers"`
	Producers        int         `json:"producers"`
	BatchSize        int         `json:"batch_size"`
	WarmupSeconds    float64     `json:"warmup_seconds"`
	WindowSeconds    float64     `json:"window_seconds"`
	HoldSeconds      float64     `json:"hold_seconds,omitempty"`
	Shards           int         `json:"shards,omitempty"`
	StartedAt        time.Time   `json:"started_at"`
	CompletedTasks   int64       `json:"completed_tasks"`
	EnqueuedTasks    int64       `json:"enqueued_tasks"`
	ThroughputPerSec float64     `json:"throughput_per_sec"`
	P50Millis        float64     `json:"p50_ms"`
	P95Millis        float64     `json:"p95_ms"`
	P99Millis        float64     `json:"p99_ms"`
	SampleCount      int         `json:"sample_count"`
	Retries          int64       `json:"retries"`
	EmptyDequeues    int64       `json:"empty_dequeues"`
	BacklogStart     int64       `json:"backlog_start"`
	BacklogEnd       int64       `json:"backlog_end"`
	Valid            bool        `json:"valid"`
	InvalidReasons   []string    `json:"invalid_reasons,omitempty"`
	Env              Environment `json:"env"`
}

type Environment struct {
	Argv           []string          `json:"argv"`
	Image          string            `json:"image,omitempty"`
	ImageDigest    string            `json:"image_digest,omitempty"`
	ServerVersion  string            `json:"server_version"`
	ServerSettings map[string]string `json:"server_settings"`
	GOOS           string            `json:"goos"`
	GOARCH         string            `json:"goarch"`
	NumCPU         int               `json:"num_cpu"`
	VMCPUs         int               `json:"vm_cpus,omitempty"`
	GitCommit      string            `json:"git_commit,omitempty"`
}

func (e Environment) Fingerprint() string {
	return strings.Join([]string{
		e.GOOS, e.GOARCH,
		fmt.Sprint(e.NumCPU), fmt.Sprint(e.VMCPUs), e.ImageDigest, e.ServerVersion,
	}, "|")
}

func WriteResult(dir string, r Result) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s-w%d.json",
		r.StartedAt.UTC().Format("20060102T150405"), r.Stage, r.Workers)
	path := filepath.Join(dir, name)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, append(data, '\n'), 0o644)
}

func ReadResults(paths []string) ([]Result, error) {
	var results []Result
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var r Result
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		results = append(results, r)
	}
	return results, nil
}

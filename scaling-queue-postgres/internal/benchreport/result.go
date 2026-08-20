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
	Benchmark string `json:"benchmark"`
	Mode      string `json:"mode"`
	Stage     string `json:"stage"`

	Workers         int     `json:"workers"`
	Completers      int     `json:"completers"`
	Producers       int     `json:"producers"`
	Slots           int     `json:"slots"`
	Shards          int     `json:"shards"`
	BatchSize       int     `json:"batch_size"`
	CreateBatch     int     `json:"create_batch"`
	CompletionBatch int     `json:"completion_batch"`
	DurationMinSecs float64 `json:"duration_min_seconds"`
	DurationMaxSecs float64 `json:"duration_max_seconds"`
	WarmupSeconds   float64 `json:"warmup_seconds"`
	WindowSeconds   float64 `json:"window_seconds"`

	StartedAt        time.Time `json:"started_at"`
	CompletedTasks   int64     `json:"completed_tasks"`
	CreatedTasks     int64     `json:"created_tasks"`
	ThroughputPerSec float64   `json:"throughput_per_sec"`

	P50Millis     float64 `json:"claim_p50_ms"`
	P95Millis     float64 `json:"claim_p95_ms"`
	P99Millis     float64 `json:"claim_p99_ms"`
	DoneP95Millis float64 `json:"done_p95_ms"`
	SampleCount   int     `json:"sample_count"`

	Retries     int64 `json:"retries"`
	EmptyClaims int64 `json:"empty_claims"`

	MeanInFlight            float64 `json:"mean_in_flight"`
	ExpectedInFlight        float64 `json:"expected_in_flight"`
	LittleErrorPercent      float64 `json:"little_error_percent"`
	LittleTolerancePercent  float64 `json:"little_tolerance_percent"`
	MeanBacklog             float64 `json:"mean_backlog"`
	BacklogVariationPercent float64 `json:"backlog_variation_percent"`
	BacklogStart            int64   `json:"backlog_start"`
	BacklogEnd              int64   `json:"backlog_end"`

	Valid          bool        `json:"valid"`
	InvalidReasons []string    `json:"invalid_reasons,omitempty"`
	Env            Environment `json:"env"`
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
	name := fmt.Sprintf("%s-%s-%s-w%d.json",
		r.StartedAt.UTC().Format("20060102T150405"), r.Mode, r.Stage, r.Workers)
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

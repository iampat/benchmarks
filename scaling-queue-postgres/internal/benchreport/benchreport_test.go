package benchreport_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/benchreport"
)

func TestPercentiles(t *testing.T) {
	ms := func(n int) time.Duration { return time.Duration(n) * time.Millisecond }

	if got := benchreport.Percentiles(nil); got.Count != 0 {
		t.Errorf("empty: %+v, want zero", got)
	}

	one := benchreport.Percentiles([]time.Duration{ms(7)})
	if one.P50 != ms(7) || one.P99 != ms(7) || one.Count != 1 {
		t.Errorf("single sample: %+v", one)
	}

	var hundred []time.Duration
	for i := 100; i >= 1; i-- {
		hundred = append(hundred, ms(i))
	}
	got := benchreport.Percentiles(hundred)
	if got.P50 != ms(50) || got.P95 != ms(95) || got.P99 != ms(99) {
		t.Errorf("1..100: p50=%v p95=%v p99=%v, want 50ms 95ms 99ms", got.P50, got.P95, got.P99)
	}

	ties := benchreport.Percentiles([]time.Duration{ms(5), ms(5), ms(5), ms(5)})
	if ties.P50 != ms(5) || ties.P99 != ms(5) {
		t.Errorf("ties: %+v", ties)
	}
}

func TestRecorderGatesOnRecordingFlag(t *testing.T) {
	r := benchreport.NewRecorder(2)

	r.Lane(0).Record(time.Millisecond)
	if n := len(r.Samples()); n != 0 {
		t.Errorf("recorded %d samples before SetRecording(true), want 0", n)
	}

	r.SetRecording(true)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lane := r.Lane(i)
			for range 100 {
				lane.Record(time.Millisecond)
			}
		}()
	}
	wg.Wait()
	r.SetRecording(false)
	r.Lane(1).Record(time.Millisecond)
	if n := len(r.Samples()); n != 200 {
		t.Errorf("got %d samples, want 200", n)
	}
}

func TestResultRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := benchreport.Result{
		Benchmark:        "scaling-queue-postgres",
		Mode:             "steady",
		Stage:            "1-skip-locked",
		Workers:          16,
		StartedAt:        time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC),
		ThroughputPerSec: 1234.5,
		MeanInFlight:     300000,
		Valid:            true,
		Env: benchreport.Environment{
			Argv:           []string{"bench", "-stages=1-skip-locked"},
			ServerVersion:  "PostgreSQL 18.0",
			ServerSettings: map[string]string{"max_connections": "200"},
			GOOS:           "darwin",
			GOARCH:         "arm64",
			NumCPU:         10,
		},
	}
	path, err := benchreport.WriteResult(dir, want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, benchreport.ResultsFile) {
		t.Errorf("unexpected file name %s", path)
	}
	// A second cell appends rather than replacing the first.
	second := want
	second.Stage = "2-read-committed"
	second.ThroughputPerSec = 99
	if _, err := benchreport.WriteResult(dir, second); err != nil {
		t.Fatal(err)
	}
	got, err := benchreport.ReadResults([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d results, want 2", len(got))
	}
	if got[0].ThroughputPerSec != want.ThroughputPerSec ||
		got[0].Stage != want.Stage || got[0].Mode != want.Mode ||
		got[0].MeanInFlight != want.MeanInFlight ||
		!got[0].StartedAt.Equal(want.StartedAt) {
		t.Errorf("round trip = %+v, want %+v", got[0], want)
	}
	if got[1].Stage != second.Stage || got[1].ThroughputPerSec != second.ThroughputPerSec {
		t.Errorf("appended result = %+v, want %+v", got[1], second)
	}
}

func TestBuildReport(t *testing.T) {
	env := benchreport.Environment{
		Argv:          []string{"bench"},
		ServerVersion: "PostgreSQL 18.0",
		GOOS:          "darwin",
		GOARCH:        "arm64",
		NumCPU:        10,
	}
	mk := func(mode, stage string, tput float64, valid bool) benchreport.Result {
		return benchreport.Result{
			Mode: mode, Stage: stage, Workers: 16, ThroughputPerSec: tput,
			Valid: valid, InvalidReasons: map[bool][]string{false: {"queue drained"}}[valid],
			Env: env,
		}
	}
	report := benchreport.BuildReport([]benchreport.Result{
		mk("steady", "0-vanilla", 100, true),
		mk("steady", "0-vanilla", 200, true),
		mk("steady", "0-vanilla", 300, true),
		mk("steady", "1-skip-locked", 400, true),
		mk("steady", "2-read-committed", 0, false),
		mk("drain", "0-vanilla", 900, true),
	})

	for _, want := range []string{
		"The task queue",
		"Cross-check",
		"#### 16 claim loops",
		"| 0-vanilla | 200 | — |",
		"| 1-skip-locked | 400 | +100% |",
		"| 0-vanilla | 900 | — |",
		"invalid: queue drained",
		"PostgreSQL 18.0",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q\n%s", want, report)
		}
	}
}

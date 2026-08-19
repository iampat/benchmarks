package benchreport

import (
	"math"
	"slices"
	"sync/atomic"
	"time"
)

type Recorder struct {
	recording atomic.Bool
	lanes     []*Lane
}

func NewRecorder(lanes int) *Recorder {
	r := &Recorder{lanes: make([]*Lane, lanes)}
	for i := range r.lanes {
		r.lanes[i] = &Lane{recording: &r.recording}
	}
	return r
}

func (r *Recorder) Lane(i int) *Lane {
	return r.lanes[i]
}

func (r *Recorder) SetRecording(on bool) {
	r.recording.Store(on)
}

// Only call after every recording goroutine has stopped.
func (r *Recorder) Samples() []time.Duration {
	var merged []time.Duration
	for _, l := range r.lanes {
		merged = append(merged, l.samples...)
	}
	return merged
}

type Lane struct {
	recording *atomic.Bool
	samples   []time.Duration
}

func (l *Lane) Record(d time.Duration) {
	if l.recording.Load() {
		l.samples = append(l.samples, d)
	}
}

type LatencyStats struct {
	Count int
	P50   time.Duration
	P95   time.Duration
	P99   time.Duration
}

func Percentiles(samples []time.Duration) LatencyStats {
	if len(samples) == 0 {
		return LatencyStats{}
	}
	s := slices.Clone(samples)
	slices.Sort(s)
	q := func(p float64) time.Duration {
		idx := int(math.Ceil(p*float64(len(s)))) - 1
		if idx < 0 {
			idx = 0
		}
		return s[idx]
	}
	return LatencyStats{Count: len(s), P50: q(0.50), P95: q(0.95), P99: q(0.99)}
}

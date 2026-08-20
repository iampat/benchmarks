package benchreport_test

import (
	"flag"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/benchreport"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/experiment"
)

func cell(step int, mode string, rate float64, valid bool) benchreport.Result {
	r := benchreport.Result{Step: step, Mode: mode, Valid: valid}
	if mode == "ops" {
		r.OpsPerSec = rate
	}
	r.ThroughputPerSec = rate
	return r
}

// A step measured at several worker counts reports the best it reached, and an
// invalid cell never wins.
func TestBestKeepsTheFastestValidCell(t *testing.T) {
	got := benchreport.Best([]benchreport.Result{
		cell(0, "steady", 10, true),
		cell(0, "steady", 30, true),
		cell(0, "steady", 20, true),
		cell(1, "steady", 999, false),
		cell(2, "ops", 500, true),
	}, "steady")

	if got[0].ThroughputPerSec != 30 {
		t.Errorf("step 0 = %v, want the best of 10, 20, 30", got[0].ThroughputPerSec)
	}
	if _, ok := got[1]; ok {
		t.Error("an invalid cell reported a rate")
	}
	if _, ok := got[2]; ok {
		t.Error("a cell from another mode leaked in")
	}
}

func TestResultTableLeavesAnUnmeasuredStepEmpty(t *testing.T) {
	steps := experiment.Steps()
	table := benchreport.RenderResultTable(steps, map[int]benchreport.Result{
		0: cell(0, "steady", 13.4, true),
		1: cell(1, "steady", 26.8, true),
	})
	lines := strings.Split(table, "\n")
	if len(lines) != len(steps)+2 {
		t.Fatalf("table has %d lines, want %d", len(lines), len(steps)+2)
	}
	if !strings.Contains(lines[2], "| 13.4 | — |") {
		t.Errorf("first measured row = %q", lines[2])
	}
	// A doubling reads as +100 percent.
	if !strings.Contains(lines[3], "+100%") {
		t.Errorf("gain row = %q", lines[3])
	}
	// Step 2 was never measured, so its cells stay empty.
	if strings.TrimSpace(lines[4]) != "| 2. `READ COMMITTED` | | | |" {
		t.Errorf("unmeasured row = %q", lines[4])
	}
}

// The bar field is sized from the longest bar. A row that overflowed it would
// push its rate out of column, which is a bug this test exists to catch.
func TestChartColumnsLineUp(t *testing.T) {
	steps := experiment.Steps()
	best := map[int]benchreport.Result{
		0: cell(0, "steady", 13, true),
		3: cell(3, "steady", 14112, true),
		9: cell(9, "steady", 340000, true),
	}
	lines := strings.Split(benchreport.RenderChart(steps, best), "\n")
	if len(lines) != len(steps)+1 {
		t.Fatalf("chart has %d lines, want %d", len(lines), len(steps)+1)
	}
	// A bar is a multi-byte rune, so width is counted in runes.
	width := utf8.RuneCountInString(lines[0])
	for i, l := range lines {
		if got := utf8.RuneCountInString(l); got != width {
			t.Errorf("line %d is %d wide, want %d: %q", i, got, width, l)
		}
	}
	if width > 80 {
		t.Errorf("chart is %d columns wide, want 80 or fewer", width)
	}
	// The fastest step draws the longest bar.
	if strings.Count(lines[10], "░") <= strings.Count(lines[4], "░") {
		t.Error("the fastest step does not have the longest bar")
	}
	// An unmeasured step still renders a row.
	if !strings.Contains(lines[2], "—") {
		t.Errorf("unmeasured row = %q", lines[2])
	}
}

func TestStepTableMatchesTheStepList(t *testing.T) {
	steps := experiment.Steps()
	table := benchreport.RenderStepTable(steps)
	for _, s := range steps {
		if !strings.Contains(table, s.Title) || !strings.Contains(table, s.Change) {
			t.Errorf("step %d is missing from the table", s.N)
		}
	}
}

func TestFlagTableCoversEveryFlag(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.Int("workers", 16, "claim loops")
	fs.String("dsn", "", "an existing server")
	table := benchreport.RenderFlagTable(fs)
	if !strings.Contains(table, "| `-workers` | `16` | claim loops |") {
		t.Errorf("workers row missing:\n%s", table)
	}
	// An empty default reads as none rather than as a blank cell.
	if !strings.Contains(table, "| `-dsn` | `none` |") {
		t.Errorf("dsn row missing:\n%s", table)
	}
}

package benchreport

import (
	"flag"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/experiment"
)

const missing = "—"

// Best keeps the fastest valid cell for each step. A step measured at several
// worker counts reports the best one it reached.
func Best(results []Result, mode string) map[int]Result {
	best := map[int]Result{}
	for _, r := range results {
		if r.Mode != mode || !r.Valid {
			continue
		}
		if cur, ok := best[r.Step]; !ok || r.ThroughputPerSec > cur.ThroughputPerSec {
			best[r.Step] = r
		}
	}
	return best
}

func rate(r Result) float64 {
	if r.Mode == "ops" {
		return r.OpsPerSec
	}
	return r.ThroughputPerSec
}

func comma(v float64) string {
	s := fmt.Sprintf("%.0f", v)
	if v < 100 {
		s = fmt.Sprintf("%.1f", v)
	}
	whole, frac, _ := strings.Cut(s, ".")
	var out []string
	for len(whole) > 3 {
		out = append([]string{whole[len(whole)-3:]}, out...)
		whole = whole[:len(whole)-3]
	}
	out = append([]string{whole}, out...)
	s = strings.Join(out, ",")
	if frac != "" {
		s += "." + frac
	}
	return s
}

// billion is how long the measured rate needs to finish a billion tasks. It is
// arithmetic on one window, not a forecast.
func billion(v float64) string {
	if v <= 0 {
		return missing
	}
	s := 1e9 / v
	switch {
	case s >= 86400*365:
		return fmt.Sprintf("%.1f years", s/(86400*365))
	case s >= 86400*2:
		return fmt.Sprintf("%.0f days", s/86400)
	case s >= 3600:
		return fmt.Sprintf("%.0f hours", s/3600)
	default:
		return fmt.Sprintf("%.0f minutes", s/60)
	}
}

func RenderStepTable(steps []experiment.Step) string {
	var b strings.Builder
	b.WriteString("| Step | Change | Source |\n| --- | --- | --- |\n")
	for _, s := range steps {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", s.Title, s.Change, s.Source)
	}
	return strings.TrimRight(b.String(), "\n")
}

func RenderResultTable(steps []experiment.Step, best map[int]Result) string {
	var b strings.Builder
	b.WriteString("| Step | rate | Gain | 1B tasks |\n| --- | ---: | ---: | ---: |\n")
	prev := 0.0
	for _, s := range steps {
		r, ok := best[s.N]
		if !ok {
			fmt.Fprintf(&b, "| %s | | | |\n", s.Title)
			continue
		}
		v := rate(r)
		gain := missing
		if prev > 0 {
			gain = fmt.Sprintf("%+.0f%%", (v/prev-1)*100)
		}
		prev = v
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", s.Title, comma(v), gain, billion(v))
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderChart draws one bar per step on a log scale. The bar field is sized
// from the longest bar, so a wide row cannot push the columns out of line. The
// marks per doubling drop when a wide range of rates would otherwise carry the
// chart past maxChartWidth.
func RenderChart(steps []experiment.Step, best map[int]Result) string {
	const maxChartWidth = 80

	nameW := 0
	for _, s := range steps {
		nameW = max(nameW, len([]rune(s.Label)))
	}
	nameW++ // keep the longest label off the bar
	numW, timeW := 6, 9
	lowest := math.Inf(1)
	for _, s := range steps {
		r, ok := best[s.N]
		if !ok || rate(r) <= 0 {
			continue
		}
		lowest = math.Min(lowest, rate(r))
		numW = max(numW, len(comma(rate(r))))
		timeW = max(timeW, len(billion(rate(r))))
	}

	marksPerDoubling, marks, barW := 3, map[int]int{}, 1
	for _, m := range []int{3, 2, 1} {
		marksPerDoubling, marks, barW = m, map[int]int{}, 1
		if math.IsInf(lowest, 1) {
			break
		}
		for _, s := range steps {
			if r, ok := best[s.N]; ok && rate(r) > 0 {
				n := int(math.Round((math.Log2(rate(r))-math.Log2(lowest))*float64(m))) + 1
				marks[s.N] = n
				barW = max(barW, n)
			}
		}
		if 2+nameW+1+barW+1+numW+1+timeW <= maxChartWidth {
			break
		}
	}
	barW++

	// The rows and the heading negotiate one width. A short bar can leave the
	// rows narrower than the heading, so the last column takes up the slack.
	const legend = "1B tasks"
	title := strings.Repeat(" ", 2+nameW+1) + "log scale, " + marksWord(marksPerDoubling) +
		" marks per doubling"
	rowW := 2 + nameW + 1 + barW + numW + 1 + timeW
	if headW := len(title) + 1 + len(legend); headW > rowW {
		timeW += headW - rowW
		rowW = headW
	}
	head := title + strings.Repeat(" ", rowW-len(title)-len(legend)) + legend

	lines := []string{head}
	for _, s := range steps {
		num, dur, bar := missing, missing, ""
		if r, ok := best[s.N]; ok {
			num, dur = comma(rate(r)), billion(rate(r))
			bar = strings.Repeat("░", marks[s.N])
		}
		lines = append(lines, fmt.Sprintf("  %-*s║%-*s%*s %*s",
			nameW, s.Label, barW, bar, numW, num, timeW, dur))
	}
	return strings.Join(lines, "\n")
}

func marksWord(n int) string {
	switch n {
	case 1:
		return "one mark"
	case 2:
		return "two"
	default:
		return "three"
	}
}

func RenderFlagTable(fs *flag.FlagSet) string {
	type row struct{ name, def, usage string }
	var rows []row
	fs.VisitAll(func(f *flag.Flag) {
		def := f.DefValue
		if def == "" {
			def = "none"
		}
		rows = append(rows, row{f.Name, def, f.Usage})
	})
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	var b strings.Builder
	b.WriteString("| Flag | Default | Meaning |\n| --- | --- | --- |\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| `-%s` | `%s` | %s |\n", r.name, r.def, r.usage)
	}
	return strings.TrimRight(b.String(), "\n")
}

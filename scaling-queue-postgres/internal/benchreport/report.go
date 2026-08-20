package benchreport

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

var modeTitles = map[string]string{
	"steady": "The task queue (insert, claim, and complete together)",
	"drain":  "Cross-check (fill the queue, then consume it)",
	"ops":    "Queue operations (enqueue and claim, one row per statement)",
}

func BuildReport(results []Result) string {
	var b strings.Builder
	b.WriteString("# scaling-queue-postgres benchmark report\n")

	byEnv := groupBy(results, func(r Result) string { return r.Env.Fingerprint() })
	for _, envKey := range sortedKeys(byEnv) {
		envResults := byEnv[envKey]
		env := envResults[0].Env
		fmt.Fprintf(&b, "\n## Environment: %s/%s, %d CPUs", env.GOOS, env.GOARCH, env.NumCPU)
		if env.VMCPUs > 0 {
			fmt.Fprintf(&b, ", %d VM CPUs", env.VMCPUs)
		}
		b.WriteString("\n\n")
		fmt.Fprintf(&b, "- Server: %s\n", env.ServerVersion)
		if env.Image != "" {
			fmt.Fprintf(&b, "- Image: %s (%s)\n", env.Image, env.ImageDigest)
		}
		for _, k := range sortedKeys(groupBy(envResults, argvLine)) {
			fmt.Fprintf(&b, "- Command: `%s`\n", k)
		}
		if len(env.ServerSettings) > 0 {
			var kv []string
			for _, k := range sortedKeys(env.ServerSettings) {
				kv = append(kv, k+"="+env.ServerSettings[k])
			}
			fmt.Fprintf(&b, "- Settings: %s\n", strings.Join(kv, ", "))
		}

		byMode := groupBy(envResults, func(r Result) string { return r.Mode })
		for _, mode := range sortedKeys(byMode) {
			title, ok := modeTitles[mode]
			if !ok {
				title = mode
			}
			fmt.Fprintf(&b, "\n### %s\n", title)

			byWorkers := groupBy(byMode[mode], func(r Result) int { return r.Workers })
			for _, w := range sortedKeys(byWorkers) {
				fmt.Fprintf(&b, "\n#### %d claim loops\n\n", w)
				if mode == "ops" {
					b.WriteString("| stage | enqueue/s | dequeue/s | operations/s | mean queue | empty claims | runs |\n")
					b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
				} else {
					b.WriteString("| stage | tasks/s | vs prev | claim p50 | claim p95 | done p95 | in flight | Little err | retries | runs |\n")
					b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
				}

				byStage := groupBy(byWorkers[w], func(r Result) string { return r.Stage })
				prev := 0.0
				for _, stage := range sortedKeys(byStage) {
					cell := byStage[stage]
					valid := slices.DeleteFunc(slices.Clone(cell), func(r Result) bool { return !r.Valid })
					if len(valid) == 0 {
						fmt.Fprintf(&b, "| %s | invalid: %s | | | | | | | | %d |\n",
							stage, strings.Join(cell[0].InvalidReasons, "; "), len(cell))
						continue
					}
					tput := median(valid, func(r Result) float64 { return r.ThroughputPerSec })
					if mode == "ops" {
						fmt.Fprintf(&b, "| %s | %.0f | %.0f | %.0f | %.0f | %.0f | %d |\n",
							stage,
							median(valid, func(r Result) float64 { return r.EnqueuePerSec }),
							tput,
							median(valid, func(r Result) float64 { return r.OpsPerSec }),
							median(valid, func(r Result) float64 { return r.MeanBacklog }),
							median(valid, func(r Result) float64 { return float64(r.EmptyClaims) }),
							len(valid))
						continue
					}
					delta := "—"
					if prev > 0 {
						delta = fmt.Sprintf("%+.0f%%", (tput/prev-1)*100)
					}
					prev = tput
					fmt.Fprintf(&b, "| %s | %.0f | %s | %.2f ms | %.2f ms | %.2f ms | %.0f | %.1f%% | %.0f | %d |\n",
						stage, tput, delta,
						median(valid, func(r Result) float64 { return r.P50Millis }),
						median(valid, func(r Result) float64 { return r.P95Millis }),
						median(valid, func(r Result) float64 { return r.DoneP95Millis }),
						median(valid, func(r Result) float64 { return r.MeanInFlight }),
						median(valid, func(r Result) float64 { return r.LittleErrorPercent }),
						median(valid, func(r Result) float64 { return float64(r.Retries) }),
						len(valid))
				}
			}
		}
	}
	b.WriteString("\nLatency percentiles are closed-loop service times, retries included.\n")
	b.WriteString("\"Little err\" is how far tasks in flight sat from throughput times mean\n")
	b.WriteString("task duration. A small number means the run reached a steady state.\n")
	return b.String()
}

func argvLine(r Result) string {
	if len(r.Env.Argv) == 0 {
		return ""
	}
	args := slices.Clone(r.Env.Argv)
	args[0] = filepath.Base(args[0])
	return strings.Join(args, " ")
}

func groupBy[K comparable](results []Result, key func(Result) K) map[K][]Result {
	m := map[K][]Result{}
	for _, r := range results {
		m[key(r)] = append(m[key(r)], r)
	}
	return m
}

func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func median(results []Result, metric func(Result) float64) float64 {
	vals := make([]float64, len(results))
	for i, r := range results {
		vals[i] = metric(r)
	}
	slices.Sort(vals)
	n := len(vals)
	if n%2 == 1 {
		return vals[n/2]
	}
	return (vals[n/2-1] + vals[n/2]) / 2
}

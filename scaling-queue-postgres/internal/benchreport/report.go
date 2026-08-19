package benchreport

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

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

		byWorkers := groupBy(envResults, func(r Result) int { return r.Workers })
		for _, w := range sortedKeys(byWorkers) {
			fmt.Fprintf(&b, "\n### %d workers\n\n", w)
			b.WriteString("| stage | tasks/s | vs prev | p50 ms | p95 ms | p99 ms | retries | empty | runs |\n")
			b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")

			byStage := groupBy(byWorkers[w], stageLabel)
			prev := 0.0
			for _, stage := range sortedKeys(byStage) {
				cell := byStage[stage]
				valid := slices.DeleteFunc(slices.Clone(cell), func(r Result) bool { return !r.Valid })
				if len(valid) == 0 {
					fmt.Fprintf(&b, "| %s | invalid: %s | | | | | | | %d |\n",
						stage, strings.Join(cell[0].InvalidReasons, "; "), len(cell))
					continue
				}
				tput := median(valid, func(r Result) float64 { return r.ThroughputPerSec })
				delta := "—"
				if prev > 0 {
					delta = fmt.Sprintf("%+.0f%%", (tput/prev-1)*100)
				}
				prev = tput
				fmt.Fprintf(&b, "| %s | %.0f | %s | %.2f | %.2f | %.2f | %.0f | %.0f | %d |\n",
					stage, tput, delta,
					median(valid, func(r Result) float64 { return r.P50Millis }),
					median(valid, func(r Result) float64 { return r.P95Millis }),
					median(valid, func(r Result) float64 { return r.P99Millis }),
					median(valid, func(r Result) float64 { return float64(r.Retries) }),
					median(valid, func(r Result) float64 { return float64(r.EmptyDequeues) }),
					len(valid))
			}
		}
	}
	b.WriteString("\nLatency percentiles are closed-loop service times, retries included.\n")
	return b.String()
}

func stageLabel(r Result) string {
	label := r.Stage
	if r.Shards > 1 {
		label += fmt.Sprintf(" (shards %d)", r.Shards)
	}
	if r.HoldSeconds > 0 {
		label += fmt.Sprintf(" (hold %gs)", r.HoldSeconds)
	}
	return label
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

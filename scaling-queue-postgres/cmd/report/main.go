package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/benchcfg"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/benchreport"
	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/experiment"
)

func main() {
	if err := run(); err != nil {
		slog.Error("report failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", "scaling-queue-postgres/results", "directory holding the results")
	docs := flag.String("docs", "scaling-queue-postgres", "directory holding README.md and METHOD.md")
	out := flag.String("out", "REPORT.md", "raw report file name, written into -dir")
	check := flag.Bool("check", false, "report whether a document is stale, and write nothing")
	flag.Parse()

	if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" {
		if !filepath.IsAbs(*dir) {
			*dir = filepath.Join(ws, *dir)
		}
		if !filepath.IsAbs(*docs) {
			*docs = filepath.Join(ws, *docs)
		}
	}

	resultsPath := filepath.Join(*dir, benchreport.ResultsFile)
	var results []benchreport.Result
	if _, err := os.Stat(resultsPath); err == nil {
		results, err = benchreport.ReadResults([]string{resultsPath})
		if err != nil {
			return err
		}
	}

	steps := experiment.Steps()
	readme := []benchreport.Block{
		{Name: "steps-table", Body: benchreport.RenderStepTable(steps)},
	}
	for _, m := range []struct{ mode, prefix string }{{"ops", "ops"}, {"steady", "task"}} {
		best := benchreport.Best(results, m.mode)
		readme = append(
			readme,
			benchreport.Block{Name: m.prefix + "-table", Body: benchreport.RenderResultTable(steps, best)},
			benchreport.Block{Name: m.prefix + "-chart", Body: "```\n" + benchreport.RenderChart(steps, best) + "\n```"},
		)
	}
	method := []benchreport.Block{
		{Name: "flags-table", Body: benchreport.RenderFlagTable(benchcfg.FlagSet())},
	}

	stale := false
	for _, doc := range []struct {
		path   string
		blocks []benchreport.Block
	}{
		{filepath.Join(*docs, "README.md"), readme},
		{filepath.Join(*docs, "METHOD.md"), method},
	} {
		changed, err := benchreport.Apply(doc.path, doc.blocks, *check)
		if err != nil {
			return err
		}
		if changed {
			stale = true
			if *check {
				fmt.Fprintf(os.Stderr, "%s is out of date, run the docs target\n", doc.path)
			} else {
				slog.Info("wrote", "path", doc.path)
			}
		}
	}

	if *check {
		if stale {
			return fmt.Errorf("a document is out of date")
		}
		slog.Info("documents are current")
		return nil
	}

	if len(results) > 0 {
		report := benchreport.BuildReport(results)
		target := filepath.Join(*dir, *out)
		if err := os.WriteFile(target, []byte(report), 0o644); err != nil {
			return err
		}
		slog.Info("wrote", "path", target, "results", len(results))
	}
	return nil
}

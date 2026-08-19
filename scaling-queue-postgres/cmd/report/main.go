package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/iampat/benchmarks/scaling-queue-postgres/internal/benchreport"
)

func main() {
	if err := run(); err != nil {
		slog.Error("report failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", "scaling-queue-postgres/results", "directory with result JSON files")
	out := flag.String("out", "REPORT.md", "report file name, written into -dir")
	flag.Parse()

	if ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY"); ws != "" && !filepath.IsAbs(*dir) {
		*dir = filepath.Join(ws, *dir)
	}

	paths := flag.Args()
	if len(paths) == 0 {
		var err error
		paths, err = filepath.Glob(filepath.Join(*dir, "*.json"))
		if err != nil {
			return err
		}
	}
	if len(paths) == 0 {
		return fmt.Errorf("no result files in %s", *dir)
	}

	results, err := benchreport.ReadResults(paths)
	if err != nil {
		return err
	}
	report := benchreport.BuildReport(results)
	fmt.Print(report)

	target := filepath.Join(*dir, *out)
	if err := os.WriteFile(target, []byte(report), 0o644); err != nil {
		return err
	}
	slog.Info("report written", "path", target, "results", len(results))
	return nil
}

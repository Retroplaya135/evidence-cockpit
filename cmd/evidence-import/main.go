package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Retroplaya135/evidence-cockpit/internal/ingest"
)

func main() {
	input := flag.String("input", "fixtures/evidence.csv", "repository-relative evidence file or directory")
	fullReport := flag.Bool("report", false, "print the complete import report")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}

	path := *input
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}

	report, err := ingest.Run(ingest.DefaultConfig(root), []string{path})
	if err != nil {
		fatal(err)
	}

	var output any = struct {
		ReportVersion string              `json:"report_version"`
		Totals        ingest.ReportTotals `json:"totals"`
	}{
		ReportVersion: report.ReportVersion,
		Totals:        report.Totals,
	}
	if *fullReport {
		output = report
	}

	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fatal(err)
	}
	fmt.Println(string(encoded))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}


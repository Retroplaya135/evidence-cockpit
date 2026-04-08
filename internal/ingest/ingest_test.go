package ingest

import (
	"path/filepath"
	"testing"
)

func TestSyntheticEvidenceRoutesByQuality(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(DefaultConfig(root), []string{filepath.Join(root, "fixtures", "evidence.csv")})
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals.FileCount != 1 || report.Totals.RowCount != 3 {
		t.Fatalf("unexpected totals: %+v", report.Totals)
	}
	if report.Totals.AcceptedRows != 1 || report.Totals.ManualReviewRows != 1 || report.Totals.QuarantinedRows != 1 {
		t.Fatalf("quality routing changed: %+v", report.Totals)
	}
	if report.Files[0].Hash == "" {
		t.Fatal("expected a stable source hash")
	}
}

func TestRejectedRootDoesNotPromote(t *testing.T) {
	root := t.TempDir()
	cfg := DefaultConfig(root)
	decision := cfg.ClassifyPath(filepath.Join(root, "private", "records.csv"))
	if decision.State != RootRejected {
		t.Fatalf("private root was not rejected: %+v", decision)
	}
}

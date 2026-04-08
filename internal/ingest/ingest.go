package ingest

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var allowedClaimReadiness = map[string]bool{
	"dashboard_ready":                         true,
	"research_backed_field_validation_needed": true,
	"hypothesis_only":                         true,
	"do_not_visualize":                        true,
}

func Run(cfg Config, paths []string) (ImportReport, error) {
	if cfg.RepoRoot == "" {
		return ImportReport{}, errors.New("repo root is required")
	}
	files, err := expandFiles(paths)
	if err != nil {
		return ImportReport{}, err
	}
	sort.Strings(files)

	report := ImportReport{
		ReportVersion: "evidence_import_report_v1",
		ParserVersion: cfg.parserVersion(),
		GeneratedAt:   cfg.runTimestamp(),
		Files:         make([]FileReport, 0, len(files)),
	}

	for _, file := range files {
		fileReport := ingestFile(cfg, file)
		report.Files = append(report.Files, fileReport)
		report.Totals.FileCount++
		report.Totals.RowCount += fileReport.RowCount
		report.Totals.AcceptedRows += fileReport.AcceptedRows
		report.Totals.ManualReviewRows += fileReport.ManualReviewRows
		report.Totals.QuarantinedRows += fileReport.QuarantinedRows
		if fileReport.RejectedReason != "" && fileReport.RowCount == 0 {
			report.Totals.RejectedFiles++
		}
	}
	return report, nil
}

func expandFiles(paths []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, path := range paths {
		clean := filepath.Clean(path)
		info, err := os.Stat(clean)
		if err != nil {
			if !seen[clean] {
				seen[clean] = true
				out = append(out, clean)
			}
			continue
		}
		if !info.IsDir() {
			if !seen[clean] {
				seen[clean] = true
				out = append(out, clean)
			}
			continue
		}
		err = filepath.WalkDir(clean, func(child string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			child = filepath.Clean(child)
			if !seen[child] {
				seen[child] = true
				out = append(out, child)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func ingestFile(cfg Config, path string) FileReport {
	decision := cfg.ClassifyPath(path)
	rel := relativePath(cfg.RepoRoot, path)
	report := FileReport{
		File:          filepath.Clean(path),
		RelativeFile:  rel,
		FileType:      strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		RootState:     string(decision.State),
		ParserVersion: cfg.parserVersion(),
	}
	report.ParserName = parserName(path)
	report.NextStageTableTarget = nextStageTarget(path, nil, decision)

	body, err := os.ReadFile(path)
	if err != nil {
		report.RejectedReason = "missing_or_unreadable_file"
		return report
	}
	report.Hash = "sha256:" + sha256Hex(body)

	if decision.State == RootRejected {
		report.RejectedReason = decision.Reason
		return report
	}
	if !supportedExtension(path) {
		report.RejectedReason = "unsupported_format_backlog"
		return report
	}

	rows, parseErrs := parseFile(path, body)
	if len(parseErrs) > 0 && len(rows) == 0 {
		report.RowCount = len(parseErrs)
		for i, parseErr := range parseErrs {
			row := RowResult{
				RowID:                fmt.Sprintf("parse_error_%d", i+1),
				RowNumber:            i + 1,
				Status:               StatusQuarantined,
				ParserName:           report.ParserName,
				ParserVersion:        cfg.parserVersion(),
				RawHash:              report.Hash,
				NextStageTableTarget: "dq.quarantine",
				Raw:                  map[string]any{"file": path},
				Normalized:           NormalizedRecord{ParserConfidence: 0.2, FreshnessStatus: "unknown"},
				Errors:               []OperatorMessage{parseErr},
			}
			report.Rows = append(report.Rows, row)
			report.QuarantinedRows++
		}
		return report
	}

	seenHashes := map[string]int{}
	for idx, raw := range rows {
		rowNumber := idx + 1
		rowID := rowID(raw, rowNumber)
		rawHash := "sha256:" + stableMapHash(raw)
		if seenHashes[rawHash] > 0 {
			rowID = fmt.Sprintf("%s#duplicate-%d", rowID, rowNumber)
			duplicate := buildRowResult(cfg, report.ParserName, rowID, rowNumber, rawHash, raw, decision, []OperatorMessage{{
				Code:    "duplicate_row_hash",
				Message: "duplicate row hash; row quarantined before staging to preserve raw immutability",
			}}, nil)
			report.Rows = append(report.Rows, duplicate)
			report.QuarantinedRows++
			report.RowCount++
			seenHashes[rawHash]++
			continue
		}
		seenHashes[rawHash]++

		row := buildRowResult(cfg, report.ParserName, rowID, rowNumber, rawHash, raw, decision, nil, nil)
		report.Rows = append(report.Rows, row)
		report.RowCount++
		switch row.Status {
		case StatusAccepted:
			report.AcceptedRows++
		case StatusManualReview:
			report.ManualReviewRows++
		case StatusQuarantined:
			report.QuarantinedRows++
		}
	}

	report.NextStageTableTarget = fileTargetFromRows(path, report.Rows, decision)
	report.SourceDateCoverage, report.ConfidenceCoverage, report.FreshnessCoverage = coverage(report.Rows)
	sort.SliceStable(report.Rows, func(i, j int) bool {
		if report.Rows[i].RowID == report.Rows[j].RowID {
			return report.Rows[i].RowNumber < report.Rows[j].RowNumber
		}
		return report.Rows[i].RowID < report.Rows[j].RowID
	})
	return report
}

func buildRowResult(cfg Config, parserName string, rowID string, rowNumber int, rawHash string, raw map[string]any, decision RootDecision, seedErrors []OperatorMessage, seedWarnings []OperatorMessage) RowResult {
	normalized := normalizeRow(raw)
	errors := append([]OperatorMessage{}, seedErrors...)
	warnings := append([]OperatorMessage{}, seedWarnings...)
	errors, warnings = validateRaw(raw, normalized, decision, errors, warnings)

	status := StatusAccepted
	if len(errors) > 0 {
		status = StatusQuarantined
	} else if len(warnings) > 0 || normalized.HumanReviewNeeded || len(normalized.Conflicts) > 0 || len(normalized.UnknownReasons) > 0 {
		status = StatusManualReview
	}
	if decision.State == RootAuditOnly {
		status = StatusQuarantined
	}
	if decision.State == RootManualOnly && status == StatusAccepted {
		status = StatusManualReview
		warnings = append(warnings, OperatorMessage{Code: "manual_only_source", Message: "source root is manual-only and cannot become an accepted import without promotion"})
	}

	switch status {
	case StatusAccepted:
		normalized.ParserConfidence = 0.95
	case StatusManualReview:
		normalized.ParserConfidence = 0.55
		normalized.HumanReviewNeeded = true
	case StatusQuarantined:
		normalized.ParserConfidence = 0.20
		normalized.HumanReviewNeeded = true
	}

	return RowResult{
		RowID:                rowID,
		RowNumber:            rowNumber,
		Status:               status,
		ParserName:           parserName,
		ParserVersion:        cfg.parserVersion(),
		RawHash:              rawHash,
		NextStageTableTarget: nextStageTarget("", raw, decision),
		Raw:                  cloneMap(raw),
		Normalized:           normalized,
		Errors:               errors,
		Warnings:             warnings,
	}
}

func validateRaw(raw map[string]any, normalized NormalizedRecord, decision RootDecision, errors []OperatorMessage, warnings []OperatorMessage) ([]OperatorMessage, []OperatorMessage) {
	if code := firstString(raw, "parse_error_code"); code != "" {
		errors = append(errors, OperatorMessage{Code: code, Message: firstString(raw, "parse_error_message")})
	}

	if decision.State == RootAuditOnly {
		errors = append(errors, OperatorMessage{Code: "audit_only_source", Message: "audit-only rows cannot promote into accepted evidence"})
	}

	if normalized.ClaimReadiness == "" {
		warnings = append(warnings, OperatorMessage{Code: "missing_claim_readiness", Message: "missing claim readiness routes row to manual review", Field: "claim_readiness"})
	} else if !allowedClaimReadiness[normalized.ClaimReadiness] {
		errors = append(errors, OperatorMessage{Code: "unsupported claim_readiness", Message: "unsupported claim_readiness value cannot be silently coerced", Field: "claim_readiness"})
	}

	if requiresSourceURL(raw) && firstString(raw, "source_url", "source_path_or_url") == "" {
		errors = append(errors, OperatorMessage{Code: "missing_source_url", Message: "missing source URL or client upload reference", Field: "source_url"})
	}

	if normalized.SourceDate.State == "invalid" {
		errors = append(errors, OperatorMessage{Code: "bad date", Message: "bad date cannot be coerced into source freshness", Field: "source_date_or_unknown"})
	}

	sourceType := normalizeEnum(firstString(raw, "source_type", "source_system"))
	if isPrivateOrUnsupportedSource(sourceType) {
		errors = append(errors, OperatorMessage{Code: "private_or_unsupported_source_type", Message: "private or unsupported source type cannot enter staging", Field: "source_type"})
	}

	if normalized.Phone.Validity == "blocked" {
		errors = append(errors, OperatorMessage{Code: normalized.Phone.Reason, Message: "private-number flags block ingestion for contact use", Field: "public_business_phone"})
	}

	if strings.EqualFold(firstString(raw, "requested_channel"), "cold_sms") && strings.EqualFold(firstString(raw, "sms_consent"), "false") {
		errors = append(errors, OperatorMessage{Code: "cold_sms_or_recording_consent_not_available", Message: "cold SMS or recording consent is unavailable; quarantine contact action", Field: "requested_channel"})
	}

	if hasMissingRequiredHeaders(raw) {
		errors = append(errors, OperatorMessage{Code: "missing_required_headers", Message: "missing_required_headers: bad CSV row cannot enter staging", Field: "missing_required_headers"})
	}

	if isBlockedRevenueClaim(raw) {
		errors = append(errors, OperatorMessage{Code: "recovered_revenue_or_roi_claim_not_validated", Message: "recovered revenue and ROI require attribution-supported proof", Field: "claim_type"})
	}

	if isCurrentnessSensitiveWithoutDate(raw, normalized) {
		errors = append(errors, OperatorMessage{Code: "stale_or_unknown_currentness_sensitive_source", Message: "currentness-sensitive source has no verified date", Field: "last_verified_date"})
	}

	if normalized.ClaimReadiness == "do_not_visualize" && firstString(raw, "blocked_reason") != "" {
		errors = append(errors, OperatorMessage{Code: firstString(raw, "blocked_reason"), Message: "do_not_visualize row stays in quality/quarantine lanes", Field: "claim_readiness"})
	}

	if len(normalized.Conflicts) > 0 {
		warnings = append(warnings, OperatorMessage{Code: "phone_conflict", Message: "phone conflict requires entity-resolution review", Field: "phone_tokens"})
	}

	if strings.Contains(strings.ToLower(firstString(raw, "suppression_status")), "unknown") {
		warnings = append(warnings, OperatorMessage{Code: "suppression_review_needed", Message: "suppression status needs owner review", Field: "suppression_status"})
	}

	if len(normalized.UnknownReasons) > 0 {
		warnings = append(warnings, OperatorMessage{Code: "unknown_or_redacted_fields", Message: "unknown or redacted fields preserved for human review"})
	}

	if strings.EqualFold(firstString(raw, "duplicate_candidate"), "true") || strings.EqualFold(firstString(raw, "requires_manual_or_rule_resolution"), "true") {
		warnings = append(warnings, OperatorMessage{Code: "duplicate_resolution_before_export", Message: "duplicate candidate requires review before export"})
	}

	return errors, warnings
}

func parseFile(path string, body []byte) ([]map[string]any, []OperatorMessage) {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		if base == "seed_corpus.json" {
			return parseSeedCorpus(body)
		}
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, []OperatorMessage{{Code: "json_parse_error", Message: err.Error()}}
		}
		return []map[string]any{raw}, nil
	case ".csv":
		return parseCSV(path, body)
	case ".jsonl":
		return parseJSONL(path, body)
	case ".md":
		return parseMarkdown(body), nil
	default:
		return nil, []OperatorMessage{{Code: "unsupported_format_backlog", Message: "unsupported formats are explicit backlog, not success"}}
	}
}

func parseSeedCorpus(body []byte) ([]map[string]any, []OperatorMessage) {
	var corpus struct {
		Fixtures []map[string]any `json:"fixtures"`
	}
	if err := json.Unmarshal(body, &corpus); err != nil {
		return nil, []OperatorMessage{{Code: "json_parse_error", Message: err.Error()}}
	}
	rows := make([]map[string]any, 0, len(corpus.Fixtures))
	for _, fixture := range corpus.Fixtures {
		row := map[string]any{}
		for key, value := range fixture {
			if key == "raw_record" {
				continue
			}
			if key == "confidence" {
				if confidence, ok := value.(map[string]any); ok {
					row["confidence"] = confidence
					row["confidence_level"] = confidence["level"]
					row["confidence_reason"] = confidence["reason"]
				}
				continue
			}
			row[key] = value
		}
		if rawRecord, ok := fixture["raw_record"].(map[string]any); ok {
			for key, value := range rawRecord {
				row[key] = value
			}
			row["raw_record"] = rawRecord
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func parseCSV(path string, body []byte) ([]map[string]any, []OperatorMessage) {
	reader := csv.NewReader(bytes.NewReader(body))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, []OperatorMessage{{Code: "csv_parse_error", Message: err.Error()}}
	}
	if len(records) == 0 {
		return nil, nil
	}
	headers := records[0]
	rows := make([]map[string]any, 0, len(records)-1)
	for _, record := range records[1:] {
		row := map[string]any{}
		for index, header := range headers {
			header = normalizeEnum(header)
			if index == len(headers)-1 && len(record) > len(headers) {
				row[header] = strings.TrimSpace(strings.Join(record[index:], ","))
			} else if index < len(record) {
				row[header] = strings.TrimSpace(record[index])
			} else {
				row[header] = ""
			}
		}
		applyReferenceArtifactDefaults(path, row)
		rows = append(rows, row)
	}
	return rows, nil
}

func parseJSONL(path string, body []byte) ([]map[string]any, []OperatorMessage) {
	rows := []map[string]any{}
	errors := []OperatorMessage{}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(text), &row); err != nil {
			rows = append(rows, map[string]any{
				"fixture_id":          fmt.Sprintf("jsonl_parse_error_%d", line),
				"line":                line,
				"raw_line":            text,
				"claim_readiness":     "do_not_visualize",
				"allowed_use":         "negative_gate_only",
				"parse_error_code":    "jsonl_parse_error",
				"parse_error_message": fmt.Sprintf("line %d: %v", line, err),
			})
			errors = append(errors, OperatorMessage{Code: "jsonl_parse_error", Message: fmt.Sprintf("line %d: %v", line, err)})
			continue
		}
		row["line"] = line
		applyReferenceArtifactDefaults(path, row)
		rows = append(rows, row)
	}
	return rows, nil
}

func parseMarkdown(body []byte) []map[string]any {
	row := map[string]any{
		"fixture_id":             "markdown_metadata",
		"source_type":            "markdown_metadata",
		"source_url":             "",
		"claim_readiness":        "hypothesis_only",
		"allowed_use":            "manual_review_only",
		"headings":               []any{},
		"source_date_or_unknown": "unknown",
	}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	headings := []any{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(line)
		if strings.HasPrefix(line, "#") {
			headings = append(headings, strings.TrimSpace(strings.TrimLeft(line, "#")))
		}
		if strings.HasPrefix(lower, "source date:") {
			row["source_date_or_unknown"] = strings.TrimSpace(strings.TrimPrefix(line, "Source Date:"))
		}
		if strings.HasPrefix(lower, "allowed use:") {
			row["allowed_use"] = strings.TrimSpace(strings.TrimPrefix(line, "Allowed Use:"))
		}
		if strings.HasPrefix(lower, "claim readiness:") {
			row["claim_readiness"] = strings.TrimSpace(strings.TrimPrefix(line, "Claim Readiness:"))
		}
		if strings.HasPrefix(lower, "source url:") {
			row["source_url"] = strings.TrimSpace(strings.TrimPrefix(line, "Source URL:"))
		}
	}
	row["headings"] = headings
	return []map[string]any{row}
}

func applyReferenceArtifactDefaults(path string, row map[string]any) {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(base, "source_registry"):
		setDefault(row, "source_type", "source_registry")
		setDefault(row, "claim_readiness", "research_backed_field_validation_needed")
		setDefault(row, "allowed_use", "source_registry_dashboard_reference")
		setDefault(row, "source_date_or_unknown", "2026-06-17")
		if firstString(row, "source_url") == "" {
			if example := firstHTTPURL(row["url_examples"]); example != "" {
				row["source_url"] = example
			}
		}
	case strings.Contains(base, "field_dictionary"):
		setDefault(row, "source_type", "field_dictionary")
		setDefault(row, "claim_readiness", "research_backed_field_validation_needed")
		setDefault(row, "allowed_use", "field_dictionary_dashboard_reference")
		setDefault(row, "source_path_or_url", "file:"+filepath.ToSlash(path))
		setDefault(row, "source_date_or_unknown", "2026-06-17")
	case strings.Contains(base, "import_expectation_map"):
		setDefault(row, "source_type", "fixture_contract")
		setDefault(row, "claim_readiness", "research_backed_field_validation_needed")
		setDefault(row, "allowed_use", "validation_contract")
		setDefault(row, "source_path_or_url", "file:"+filepath.ToSlash(path))
		setDefault(row, "source_date_or_unknown", "2026-06-17")
	case strings.Contains(base, "source_quality"):
		setDefault(row, "source_type", "source_quality_model")
		setDefault(row, "claim_readiness", "research_backed_field_validation_needed")
		setDefault(row, "allowed_use", "source_quality_dashboard_reference")
		setDefault(row, "source_path_or_url", "file:"+filepath.ToSlash(path))
		setDefault(row, "source_date_or_unknown", "2026-06-17")
	}
}

func setDefault(row map[string]any, key string, value any) {
	if firstString(row, key) == "" {
		row[key] = value
	}
}

func firstHTTPURL(value any) string {
	for _, candidate := range stringSlice(value) {
		candidate = strings.TrimSpace(candidate)
		if strings.HasPrefix(strings.ToLower(candidate), "http://") || strings.HasPrefix(strings.ToLower(candidate), "https://") {
			return candidate
		}
	}
	return ""
}

func supportedExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".csv", ".jsonl", ".md":
		return true
	default:
		return false
	}
}

func parserName(path string) string {
	base := filepath.Base(path)
	switch {
	case base == "seed_corpus.json":
		return "parse_seed_corpus_json"
	case strings.HasSuffix(base, ".json"):
		return "parse_json_document"
	case strings.HasSuffix(base, ".jsonl"):
		return "parse_jsonl_source_registry"
	case strings.HasSuffix(base, ".md"):
		return "parse_markdown_metadata"
	case strings.Contains(base, "source_manifest"):
		return "parse_source_manifest_gate"
	case strings.Contains(base, "import_expectation_map"):
		return "parse_import_expectation_map"
	case strings.Contains(base, "field_dictionary"):
		return "parse_field_dictionary_csv"
	case strings.Contains(base, "source_quality"):
		return "parse_source_quality_model_csv"
	default:
		return "parse_csv_records"
	}
}

func nextStageTarget(path string, raw map[string]any, decision RootDecision) string {
	if decision.State == RootAuditOnly {
		return "dq.validation_gate_runs"
	}
	if raw == nil {
		return "stg.parsed_records"
	}
	if decision.State == RootManualOnly {
		return "dq.manual_review"
	}
	if firstString(raw, "promotion_allowed") == "no" || firstString(raw, "promotion_allowed") == "false" {
		return "dq.validation_gate_runs"
	}
	if firstString(raw, "blocked_reason") != "" || firstString(raw, "claim_readiness") == "do_not_visualize" {
		return "dq.quarantine"
	}
	switch normalizeEnum(firstString(raw, "source_type")) {
	case "source_registry":
		return "stg.normalized_records -> evidence.source_registry"
	case "field_dictionary":
		return "stg.normalized_records -> intel.field_dictionary"
	case "source_quality_model":
		return "stg.normalized_records -> intel.source_quality"
	case "fixture_contract":
		return "dq.validation_outcomes"
	}
	tables := stringSlice(raw["expected_table"])
	if len(tables) > 0 {
		if containsString(tables, "quality_gate_run") {
			return "dq.validation_gate_runs"
		}
		if containsString(tables, "client_sample") || containsString(tables, "diagnostic_record") {
			return "stg.normalized_records -> sales.*"
		}
		if containsString(tables, "competitor") || containsString(tables, "workflow_bottleneck") {
			return "stg.normalized_records -> intel.*"
		}
		return "stg.normalized_records -> core.*"
	}
	if strings.Contains(filepath.Base(path), "field_dictionary") || strings.Contains(filepath.Base(path), "source_quality") {
		return "stg.normalized_records -> intel.source_quality"
	}
	if strings.Contains(filepath.Base(path), "import_expectation_map") {
		return "dq.validation_outcomes"
	}
	return "stg.normalized_records"
}

func fileTargetFromRows(path string, rows []RowResult, decision RootDecision) string {
	if decision.State == RootAuditOnly {
		return "dq.validation_gate_runs"
	}
	targets := map[string]bool{}
	for _, row := range rows {
		targets[row.NextStageTableTarget] = true
	}
	if len(targets) == 1 {
		for target := range targets {
			return target
		}
	}
	if len(targets) == 0 {
		return nextStageTarget(path, nil, decision)
	}
	return "mixed: raw.source_records + stg.parsed_records + dq.*"
}

func coverage(rows []RowResult) (float64, float64, float64) {
	if len(rows) == 0 {
		return 0, 0, 0
	}
	sourceDate := 0
	confidence := 0
	freshness := 0
	for _, row := range rows {
		if row.Normalized.SourceDate.State != "" {
			sourceDate++
		}
		if row.Normalized.SourceConfidence > 0 {
			confidence++
		}
		if row.Normalized.FreshnessStatus != "" {
			freshness++
		}
	}
	total := float64(len(rows))
	return float64(sourceDate) / total, float64(confidence) / total, float64(freshness) / total
}

func rowID(raw map[string]any, rowNumber int) string {
	for _, key := range []string{"fixture_id", "source_id", "field_name", "lane_id", "check_id", "artifact_path", "record_id"} {
		if value := firstString(raw, key); value != "" {
			return value
		}
	}
	return fmt.Sprintf("row_%d", rowNumber)
}

func stableMapHash(raw map[string]any) string {
	hashable := cloneMap(raw)
	delete(hashable, "row_number")
	delete(hashable, "line")
	body, _ := json.Marshal(hashable)
	return sha256Hex(body)
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func relativePath(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Clean(path)
	}
	return rel
}

func requiresSourceURL(raw map[string]any) bool {
	sourceType := normalizeEnum(firstString(raw, "source_type", "source_system"))
	if sourceType == "client_export" || sourceType == "client_upload" {
		return false
	}
	if firstString(raw, "artifact_path", "source_path_or_url", "url_examples", "source_id", "field_name") != "" {
		return false
	}
	return firstString(raw, "source_url") == ""
}

func isPrivateOrUnsupportedSource(sourceType string) bool {
	switch sourceType {
	case "people_search", "personal_enrichment", "personal_mobile", "private_number", "aggregator_unverified":
		return true
	default:
		return false
	}
}

func hasMissingRequiredHeaders(raw map[string]any) bool {
	return len(stringSlice(raw["missing_required_headers"])) > 0
}

func isBlockedRevenueClaim(raw map[string]any) bool {
	text := strings.ToLower(firstString(raw, "claim_type", "claim_text_token", "requested_visualization", "blocked_claim_probe"))
	return strings.Contains(text, "recovered_revenue") || strings.Contains(text, "roi") || strings.Contains(text, "guaranteed_recovered_revenue")
}

func isCurrentnessSensitiveWithoutDate(raw map[string]any, normalized NormalizedRecord) bool {
	currentness := strings.EqualFold(firstString(raw, "currentness_sensitive"), "true")
	if !currentness {
		return false
	}
	return normalized.SourceDate.State == "unknown" || normalized.SourceDate.State == "invalid"
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

package ingest

const DefaultParserVersion = "evidence_ingest_v1"

type Status string

const (
	StatusAccepted     Status = "accepted"
	StatusManualReview Status = "manual_review"
	StatusQuarantined  Status = "quarantined"
	StatusRejected     Status = "rejected"
)

type RootState string

const (
	RootAccepted   RootState = "accepted"
	RootAuditOnly  RootState = "audit_only"
	RootManualOnly RootState = "manual_only"
	RootRejected   RootState = "rejected"
)

type Config struct {
	RepoRoot      string
	AcceptedRoots []string
	ManualRoots   []string
	RejectedRoots []string
	ParserVersion string
	RunTimestamp  string
}

type RootDecision struct {
	State  RootState `json:"state"`
	Reason string    `json:"reason,omitempty"`
}

type ImportReport struct {
	ReportVersion string       `json:"report_version"`
	ParserVersion string       `json:"parser_version"`
	GeneratedAt   string       `json:"generated_at"`
	Files         []FileReport `json:"files"`
	Totals        ReportTotals `json:"totals"`
}

type ReportTotals struct {
	FileCount        int `json:"file_count"`
	RowCount         int `json:"row_count"`
	AcceptedRows     int `json:"accepted_rows"`
	ManualReviewRows int `json:"manual_review_rows"`
	QuarantinedRows  int `json:"quarantined_rows"`
	RejectedFiles    int `json:"rejected_files"`
}

type FileReport struct {
	File                 string      `json:"file"`
	RelativeFile         string      `json:"relative_file"`
	FileType             string      `json:"file_type"`
	RootState            string      `json:"root_state"`
	RejectedReason       string      `json:"rejected_reason,omitempty"`
	ParserName           string      `json:"parser_name"`
	ParserVersion        string      `json:"parser_version"`
	Hash                 string      `json:"hash,omitempty"`
	RowCount             int         `json:"row_count"`
	AcceptedRows         int         `json:"accepted_rows"`
	ManualReviewRows     int         `json:"manual_review_rows"`
	QuarantinedRows      int         `json:"quarantined_rows"`
	SourceDateCoverage   float64     `json:"source_date_coverage"`
	ConfidenceCoverage   float64     `json:"confidence_coverage"`
	FreshnessCoverage    float64     `json:"freshness_coverage"`
	NextStageTableTarget string      `json:"next_stage_table_target"`
	Rows                 []RowResult `json:"rows"`
}

type RowResult struct {
	RowID                string            `json:"row_id"`
	RowNumber            int               `json:"row_number"`
	Status               Status            `json:"status"`
	ParserName           string            `json:"parser_name"`
	ParserVersion        string            `json:"parser_version"`
	RawHash              string            `json:"raw_hash"`
	NextStageTableTarget string            `json:"next_stage_table_target"`
	Raw                  map[string]any    `json:"raw"`
	Normalized           NormalizedRecord  `json:"normalized"`
	Errors               []OperatorMessage `json:"errors,omitempty"`
	Warnings             []OperatorMessage `json:"warnings,omitempty"`
}

type OperatorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

type NormalizedRecord struct {
	RawFields         map[string]any    `json:"raw_fields"`
	NormalizedFields  map[string]any    `json:"normalized_fields"`
	ParserConfidence  float64           `json:"parser_confidence"`
	SourceConfidence  float64           `json:"source_confidence"`
	FreshnessStatus   string            `json:"freshness_status"`
	ClaimReadiness    string            `json:"claim_readiness"`
	AllowedUse        string            `json:"allowed_use"`
	SourceStatus      string            `json:"source_status"`
	SourceDate        DateValue         `json:"source_date"`
	Phone             PhoneValue        `json:"phone"`
	URL               URLValue          `json:"url"`
	BusinessName      BusinessNameValue `json:"business_name"`
	Address           AddressValue      `json:"address"`
	Geography         GeographyValue    `json:"geography"`
	Money             MoneyValue        `json:"money"`
	EnumValues        map[string]string `json:"enum_values"`
	UnknownReasons    []string          `json:"unknown_reasons"`
	Conflicts         []Conflict        `json:"conflicts"`
	HumanReviewNeeded bool              `json:"human_review_needed"`
}

type PhoneValue struct {
	Raw        string `json:"raw,omitempty"`
	Normalized string `json:"normalized,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Validity   string `json:"validity,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type URLValue struct {
	Raw             string `json:"raw,omitempty"`
	Normalized      string `json:"normalized,omitempty"`
	CanonicalDomain string `json:"canonical_domain,omitempty"`
	PathType        string `json:"path_type,omitempty"`
}

type BusinessNameValue struct {
	Raw       string   `json:"raw,omitempty"`
	Canonical string   `json:"canonical,omitempty"`
	Tokens    []string `json:"tokens,omitempty"`
}

type AddressValue struct {
	Raw        string `json:"raw,omitempty"`
	Normalized string `json:"normalized,omitempty"`
	City       string `json:"city,omitempty"`
	State      string `json:"state,omitempty"`
}

type GeographyValue struct {
	RawState string `json:"raw_state,omitempty"`
	State    string `json:"state,omitempty"`
	RawMetro string `json:"raw_metro,omitempty"`
	Metro    string `json:"metro,omitempty"`
}

type MoneyValue struct {
	Raw             string `json:"raw,omitempty"`
	NormalizedCents int64  `json:"normalized_cents,omitempty"`
	Band            string `json:"band,omitempty"`
}

type DateValue struct {
	Raw        string `json:"raw,omitempty"`
	Normalized string `json:"normalized,omitempty"`
	State      string `json:"state"`
	Reason     string `json:"reason,omitempty"`
}

type Conflict struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
	Reason string   `json:"reason"`
}

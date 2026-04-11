package scoring

type LeadRecord struct {
	BusinessName      string
	PublicPhone       string
	State             string
	HasWebsite        bool
	HasFinancingPage  bool
	ReviewCount       int
	SourceConfidence  float64
	ComplianceRisk    float64
	PaidDemandSignals float64
}

type LeadScore struct {
	BusinessName string             `json:"business_name"`
	Score        float64            `json:"score"`
	Reasons      []string           `json:"reasons"`
	Components   map[string]float64 `json:"components"`
}

func ScoreLead(record LeadRecord) LeadScore {
	components := map[string]float64{
		"public_phone":        boolScore(record.PublicPhone != "") * 20,
		"website":             boolScore(record.HasWebsite) * 10,
		"financing_page":      boolScore(record.HasFinancingPage) * 15,
		"review_depth":        bounded(float64(record.ReviewCount)/300.0) * 15,
		"source_confidence":   bounded(record.SourceConfidence) * 20,
		"paid_demand_signals": bounded(record.PaidDemandSignals) * 15,
		"compliance_safety":   (1 - bounded(record.ComplianceRisk)) * 5,
	}

	score := 0.0
	for _, value := range components {
		score += value
	}

	return LeadScore{
		BusinessName: record.BusinessName,
		Score:        score,
		Reasons:      leadReasons(record),
		Components:   components,
	}
}

func leadReasons(record LeadRecord) []string {
	reasons := make([]string, 0, 4)
	if record.PublicPhone != "" {
		reasons = append(reasons, "public business phone present")
	}
	if record.HasFinancingPage {
		reasons = append(reasons, "financing signal present")
	}
	if record.ReviewCount >= 100 {
		reasons = append(reasons, "sufficient review depth for demand signal")
	}
	if record.ComplianceRisk > 0.5 {
		reasons = append(reasons, "high compliance risk requires manual review")
	}
	return reasons
}

func boolScore(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func bounded(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

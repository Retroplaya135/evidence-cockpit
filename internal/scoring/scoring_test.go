package scoring

import "testing"

func TestScoreIsBoundedAndExplained(t *testing.T) {
	result := ScoreLead(LeadRecord{
		BusinessName:      "Example Service Company",
		PublicPhone:       "PUBLIC_BUSINESS_PHONE_TOKEN_A",
		HasWebsite:        true,
		HasFinancingPage:  true,
		ReviewCount:       450,
		SourceConfidence:  2,
		ComplianceRisk:    -1,
		PaidDemandSignals: 2,
	})
	if result.Score < 0 || result.Score > 100 {
		t.Fatalf("score outside documented bounds: %v", result.Score)
	}
	if len(result.Reasons) == 0 || len(result.Components) != 7 {
		t.Fatalf("score lacks review evidence: %+v", result)
	}
}

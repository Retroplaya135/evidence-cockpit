package ingest

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var nonWord = regexp.MustCompile(`[^a-z0-9]+`)
var spaceRun = regexp.MustCompile(`\s+`)

func normalizeRow(raw map[string]any) NormalizedRecord {
	record := NormalizedRecord{
		RawFields:        cloneMap(raw),
		NormalizedFields: map[string]any{},
		EnumValues:       map[string]string{},
		UnknownReasons:   []string{},
		Conflicts:        []Conflict{},
	}

	record.ClaimReadiness = normalizeEnum(firstString(raw, "claim_readiness"))
	record.AllowedUse = normalizeEnum(firstString(raw, "allowed_use"))
	record.SourceStatus = normalizeSourceStatus(raw)
	record.SourceConfidence = confidenceScore(raw)
	record.SourceDate = normalizeDate(firstString(raw, "source_date_or_unknown", "source_date", "last_verified_date"))
	record.FreshnessStatus = freshnessStatus(record.SourceDate, firstString(raw, "freshness_risk"))
	record.Phone = normalizePhone(firstString(raw, "public_business_phone", "public_phone_token", "phone_token", "contact_token"))
	record.URL = normalizeURL(firstString(raw, "source_url", "source_path_or_url", "primary_website", "url"))
	record.BusinessName = normalizeBusinessName(firstString(raw, "company_name", "business_name", "entity_token", "competitor_token"))
	record.Address = normalizeAddress(firstString(raw, "address", "address_raw"))
	record.Geography = normalizeGeography(firstString(raw, "state", "service_state"), firstString(raw, "metro", "service_metro"))
	record.Money = normalizeMoney(firstString(raw, "amount", "estimate_amount", "estimate_amount_band"))

	for _, key := range []string{"claim_readiness", "allowed_use", "source_type", "decision_route", "case_type"} {
		if value := firstString(raw, key); value != "" {
			record.EnumValues[key] = normalizeEnum(value)
		}
	}

	for key, value := range raw {
		if isRedacted(value) {
			record.UnknownReasons = appendUnique(record.UnknownReasons, "redacted_value")
			record.NormalizedFields[key] = "redacted"
			continue
		}
		if value == nil && !isBenignNullField(key) {
			record.UnknownReasons = appendUnique(record.UnknownReasons, "null_"+normalizeEnum(key))
		}
		if strings.EqualFold(toString(value), "unknown") {
			record.UnknownReasons = appendUnique(record.UnknownReasons, "unknown_"+normalizeEnum(key))
		}
		record.NormalizedFields[key] = normalizeAny(value)
	}

	if tokens := stringSlice(raw["phone_tokens"]); len(tokens) > 1 {
		record.Conflicts = append(record.Conflicts, Conflict{
			Field:  "phone",
			Values: tokens,
			Reason: "multiple phone tokens disagree across sources",
		})
	}
	sort.Strings(record.UnknownReasons)
	return record
}

func normalizePhone(raw string) PhoneValue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return PhoneValue{Validity: "unknown", Reason: "missing_phone"}
	}
	upper := strings.ToUpper(raw)
	if strings.Contains(upper, "PRIVATE") || strings.Contains(upper, "PERSONAL") || strings.Contains(upper, "CELL") {
		return PhoneValue{Raw: raw, Normalized: raw, Kind: "private_or_ambiguous", Validity: "blocked", Reason: "private_or_personal_contact"}
	}
	if strings.Contains(upper, "TOKEN") {
		kind := "public_business_phone_token"
		if strings.Contains(upper, "UNKNOWN") || strings.Contains(upper, "UNVERIFIED") {
			kind = "publication_status_unknown_token"
		}
		return PhoneValue{Raw: raw, Normalized: upper, Kind: kind, Validity: "symbolic_fixture", Reason: "symbolic_non_numeric_fixture"}
	}

	digits := make([]rune, 0, len(raw))
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	if len(digits) == 10 {
		return PhoneValue{Raw: raw, Normalized: "+1" + string(digits), Kind: "public_business_phone", Validity: "valid_us_like"}
	}
	if len(digits) == 11 && digits[0] == '1' {
		return PhoneValue{Raw: raw, Normalized: "+" + string(digits), Kind: "public_business_phone", Validity: "valid_us_like"}
	}
	return PhoneValue{Raw: raw, Normalized: raw, Kind: "unknown", Validity: "invalid", Reason: "unparseable_phone"}
}

func normalizeURL(raw string) URLValue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return URLValue{}
	}
	if strings.HasPrefix(raw, "client://") {
		return URLValue{Raw: raw, Normalized: raw, CanonicalDomain: "client_upload", PathType: "client_upload"}
	}
	if !strings.HasPrefix(strings.ToLower(raw), "http://") && !strings.HasPrefix(strings.ToLower(raw), "https://") {
		return URLValue{Raw: raw, Normalized: raw, PathType: "path_or_non_url"}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return URLValue{Raw: raw, Normalized: raw, PathType: "invalid_url"}
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	host = strings.TrimPrefix(host, "www.")
	query := parsed.Query()
	for key := range query {
		if strings.HasPrefix(strings.ToLower(key), "utm_") || strings.EqualFold(key, "fbclid") || strings.EqualFold(key, "gclid") {
			query.Del(key)
		}
	}
	parsed.Host = host
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return URLValue{Raw: raw, Normalized: parsed.String(), CanonicalDomain: host, PathType: "public_url"}
}

func normalizeBusinessName(raw string) BusinessNameValue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return BusinessNameValue{}
	}
	lower := strings.ToLower(raw)
	lower = strings.ReplaceAll(lower, "&", " and ")
	lower = strings.ReplaceAll(lower, "h.v.a.c.", "hvac")
	clean := strings.TrimSpace(spaceRun.ReplaceAllString(nonWord.ReplaceAllString(lower, " "), " "))
	parts := strings.Fields(clean)
	suffixes := map[string]bool{"llc": true, "inc": true, "incorporated": true, "corp": true, "corporation": true, "co": true, "company": true, "ltd": true}
	if len(parts) > 0 && suffixes[parts[len(parts)-1]] {
		parts = parts[:len(parts)-1]
	}
	return BusinessNameValue{Raw: raw, Canonical: strings.Join(parts, " "), Tokens: parts}
}

func normalizeAddress(raw string) AddressValue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AddressValue{}
	}
	normalized := strings.TrimSpace(spaceRun.ReplaceAllString(raw, " "))
	parts := strings.Split(normalized, ",")
	value := AddressValue{Raw: raw, Normalized: normalized}
	if len(parts) >= 2 {
		value.City = strings.TrimSpace(parts[len(parts)-2])
	}
	if len(parts) >= 1 {
		state := strings.TrimSpace(parts[len(parts)-1])
		if len(state) >= 2 {
			value.State = strings.ToUpper(state[:2])
		}
	}
	return value
}

func normalizeGeography(rawState string, rawMetro string) GeographyValue {
	return GeographyValue{
		RawState: rawState,
		State:    strings.ToUpper(strings.TrimSpace(rawState)),
		RawMetro: rawMetro,
		Metro:    strings.TrimSpace(rawMetro),
	}
}

func normalizeMoney(raw string) MoneyValue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return MoneyValue{}
	}
	lower := strings.ToLower(raw)
	if lower == "unknown" || lower == "medium" || lower == "low" || lower == "high" {
		return MoneyValue{Raw: raw, Band: lower}
	}
	clean := strings.NewReplacer("$", "", ",", "", " ", "").Replace(raw)
	value, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return MoneyValue{Raw: raw, Band: "unparseable"}
	}
	return MoneyValue{Raw: raw, NormalizedCents: int64(value * 100)}
}

func normalizeDate(raw string) DateValue {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "unknown") || strings.EqualFold(raw, "null") {
		return DateValue{Raw: raw, State: "unknown", Reason: "missing_or_unknown"}
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return DateValue{Raw: raw, State: "invalid", Reason: "bad date"}
	}
	return DateValue{Raw: raw, Normalized: parsed.Format("2006-01-02"), State: "known"}
}

func freshnessStatus(date DateValue, risk string) string {
	if date.State == "invalid" {
		return "unknown"
	}
	if date.State == "unknown" {
		if strings.EqualFold(risk, "high") {
			return "stale"
		}
		return "unknown"
	}
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "high":
		return "stale"
	case "unknown":
		return "unknown"
	default:
		return "current"
	}
}

func normalizeEnum(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.ReplaceAll(raw, "-", "_")
	raw = strings.ReplaceAll(raw, " ", "_")
	raw = nonWord.ReplaceAllString(raw, "_")
	raw = strings.Trim(raw, "_")
	return raw
}

func normalizeSourceStatus(raw map[string]any) string {
	if value := firstString(raw, "promotion_allowed"); strings.EqualFold(value, "false") || strings.EqualFold(value, "no") {
		return "blocked"
	}
	if strings.Contains(strings.ToLower(firstString(raw, "synthetic_or_source_backed")), "blocked") {
		return "blocked"
	}
	if firstString(raw, "source_path_or_url", "source_url") == "" {
		return "missing_source"
	}
	return "sourced"
}

func confidenceScore(raw map[string]any) float64 {
	level := strings.ToLower(firstString(raw, "confidence_level", "confidence"))
	if confidence, ok := raw["confidence"].(map[string]any); ok {
		level = strings.ToLower(toString(confidence["level"]))
	}
	switch level {
	case "verified", "high":
		return 0.9
	case "sourced", "medium":
		return 0.7
	case "low":
		return 0.45
	case "blocked", "rejected":
		return 0
	default:
		return 0.2
	}
}

func normalizeAny(value any) any {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeAny(item))
		}
		return out
	default:
		return value
	}
}

func isRedacted(value any) bool {
	text := strings.ToUpper(strings.TrimSpace(toString(value)))
	return text == "REDACTED" || strings.Contains(text, "REDACTED")
}

func isBenignNullField(key string) bool {
	switch normalizeEnum(key) {
	case "blocked_reason":
		return true
	default:
		return false
	}
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := raw[key]; ok {
			text := toString(value)
			if strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

func toString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case []string:
		return strings.Join(v, ",")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, toString(item))
		}
		return strings.Join(parts, ",")
	default:
		body, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(body)
	}
}

func stringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, toString(item))
		}
		return out
	default:
		if text := toString(v); text != "" {
			return []string{text}
		}
		return nil
	}
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

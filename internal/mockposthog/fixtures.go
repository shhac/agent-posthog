package mockposthog

import "net/http"

func writeOrganizations(w http.ResponseWriter, r *http.Request) {
	writePage(w, r, []any{
		map[string]any{"id": "org_1", "name": "Acme", "slug": "acme"},
		map[string]any{"id": "org_2", "name": "Beta", "slug": "beta"},
	})
}

func writePersons(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("email") == "missing@example.com" || r.URL.Query().Get("distinct_id") == "missing" {
		writePage(w, r, []any{})
		return
	}
	writePage(w, r, []any{person()})
}

func eventDefinitions() []any {
	return []any{
		map[string]any{"id": 1, "name": "$pageview", "last_seen_at": "2026-05-31T12:00:00Z", "volume_30_day": 42000},
		map[string]any{"id": 2, "name": "signup completed", "last_seen_at": "2026-05-31T11:30:00Z", "volume_30_day": 700},
		map[string]any{"id": 3, "name": "checkout started", "last_seen_at": "2026-05-31T11:00:00Z", "volume_30_day": 500},
	}
}

func propertyDefinitions() []any {
	return []any{
		map[string]any{"id": 10, "name": "$browser", "type": "event", "property_type": "String"},
		map[string]any{"id": 11, "name": "$current_url", "type": "event", "property_type": "String"},
		map[string]any{"id": 12, "name": "email", "type": "person", "property_type": "String"},
		map[string]any{"id": 13, "name": "plan", "type": "person", "property_type": "String"},
	}
}

func checkoutFlag() map[string]any {
	return map[string]any{
		"id":                           22,
		"key":                          "checkout-v2",
		"name":                         "Checkout v2",
		"active":                       true,
		"rollout_percentage":           50,
		"filters":                      map[string]any{"groups": []map[string]any{{"properties": []any{}, "rollout_percentage": 50}}},
		"multivariate":                 map[string]any{"variants": []map[string]any{{"key": "control", "rollout_percentage": 50}, {"key": "treatment", "rollout_percentage": 50}}},
		"ensure_experience_continuity": true,
	}
}

func billingFlag(id int) map[string]any {
	return map[string]any{"id": id, "key": "ambiguous-flag", "name": "Ambiguous flag", "active": true}
}

func flagActivity() []any {
	return []any{
		map[string]any{
			"id":       "activity_1",
			"activity": "updated",
			"scope":    "FeatureFlag",
			"item_id":  "22",
			"detail": map[string]any{
				"changes": []map[string]any{
					{"type": "FeatureFlag", "action": "changed", "field": "active", "before": false, "after": true},
				},
			},
		},
	}
}

func experiments() []any {
	return []any{
		map[string]any{"id": 31, "name": "Draft onboarding copy", "feature_flag_key": "onboarding-copy", "archived": false, "start_date": nil, "end_date": nil, "metrics": []any{}},
		map[string]any{"id": 33, "name": "Signup CTA", "feature_flag_key": "checkout-v2", "archived": false, "start_date": "2026-05-01T00:00:00Z", "end_date": nil, "metrics": []map[string]any{{"kind": "ExperimentMetric", "metric_type": "funnel", "name": "signup conversion"}}},
		map[string]any{"id": 34, "name": "Completed pricing page", "feature_flag_key": "pricing-page", "archived": true, "start_date": "2026-01-01T00:00:00Z", "end_date": "2026-02-01T00:00:00Z", "metrics": []map[string]any{{"kind": "ExperimentMetric", "metric_type": "trend", "name": "revenue"}}},
	}
}

func person() map[string]any {
	return map[string]any{"id": 44, "uuid": "person-uuid", "distinct_ids": []string{"user_123"}, "properties": map[string]any{"email": "user@example.com", "plan": "pro"}}
}

func personActivity() []any {
	return []any{
		map[string]any{"event": "$pageview", "timestamp": "2026-05-31T12:00:00Z", "properties": map[string]any{"$current_url": "https://example.com/pricing"}},
		map[string]any{"event": "signup completed", "timestamp": "2026-05-31T12:05:00Z", "properties": map[string]any{"plan": "pro"}},
	}
}

func recording() map[string]any {
	return map[string]any{"id": "rec_1", "person_id": 44, "viewed": false, "recording_duration": 88, "start_time": "2026-05-31T12:00:00Z", "console_error_count": 2}
}

func approvalFlag() map[string]any {
	return map[string]any{"id": 26, "key": approvalFlagKey, "name": "Approval gated", "active": false, "version": 1}
}

// encryptedFlag is a remote-config flag with encrypted payloads, which the CLI
// must never rewrite.
func encryptedFlag() map[string]any {
	return map[string]any{
		"id": 28, "key": "secret-config", "name": "Encrypted config", "active": true, "version": 1,
		"is_remote_configuration": true, "has_encrypted_payloads": true,
		"filters": map[string]any{
			"groups":   []any{map[string]any{"properties": []any{}, "rollout_percentage": 100}},
			"payloads": map[string]any{"true": `{"api_secret":"s3cret"}`},
		},
	}
}

// v2Flag stores its filters in PostHog's config version 2 format, which is
// edited only as a whole document.
func v2Flag() map[string]any {
	return map[string]any{
		"id": 29, "key": "v2-flag", "name": "Config v2", "active": false, "version": 3,
		"filters": map[string]any{"version": 2, "rules": []any{map[string]any{"when": "email ends_with @example.com", "serve": true}}},
	}
}

// legacyFlag predates flag versioning, so PostHog reports its version as null.
func legacyFlag() map[string]any {
	return map[string]any{"id": 30, "key": "legacy-flag", "name": "Before versions", "active": true, "version": nil}
}

func policyFlag() map[string]any {
	return map[string]any{"id": 27, "key": policyFlagKey, "name": "Policy conflicted", "active": false, "version": 1}
}

// Fixture projects. 123 is the default; 124 stands in for a separate staging
// project; copying to 125 is gated by an approval policy.
const (
	approvalProject     = 125
	tagsRequiredProject = 126
	approvalFlagKey     = "approval-flag"
	policyFlagKey       = "policy-flag"
)

var projectTokens = map[int]string{123: "phc_mock_project_123", 124: "phc_mock_project_124"}

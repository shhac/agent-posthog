package mockposthog

import (
	"fmt"
	"net/http"
	"strings"
)

// handlePublicFlags serves POST /flags, authenticated by a project token in the
// body rather than a personal API key.
func (s *flagStore) handlePublicFlags(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	project := 0
	for p, token := range projectTokens {
		if body["api_key"] == token {
			project = p
		}
	}
	if project == 0 {
		writeStatus(w, http.StatusUnauthorized, map[string]any{"type": "authentication_error", "detail": "Project API key invalid."})
		return
	}
	distinctID, _ := body["distinct_id"].(string)
	if distinctID == "mock-error" {
		write(w, map[string]any{"flags": map[string]any{}, "errorsWhileComputingFlags": true})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	props := personProperties(distinctID, asObject(body["person_properties"]))
	out := map[string]any{}
	failures := false
	for _, id := range sortedIDs(s.flags[project]) {
		flag := s.flags[project][id]
		if flag["active"] != true || flag["deleted"] == true {
			continue
		}
		result, err := evaluateFlag(flag, props)
		if err != nil {
			failures = true
			out[fmt.Sprint(flag["key"])] = map[string]any{
				"key": flag["key"], "enabled": false, "variant": nil, "failed": true,
				"reason": map[string]any{"code": "evaluation_error", "description": err.Error()},
			}
			continue
		}
		var variant any
		if result.variant != "" {
			variant = result.variant
		}
		out[fmt.Sprint(flag["key"])] = map[string]any{
			"key": flag["key"], "enabled": result.enabled, "variant": variant, "failed": false,
			"reason":   map[string]any{"code": result.reason, "condition_index": result.conditionIndex, "description": result.reason},
			"metadata": map[string]any{"id": flag["id"], "version": flag["version"], "payload": result.payload},
		}
	}
	write(w, map[string]any{"flags": out, "errorsWhileComputingFlags": failures, "requestId": "req_mock"})
}

func testEvaluation(w http.ResponseWriter, r *http.Request, flag map[string]any) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	distinctID, _ := body["distinct_id"].(string)
	if distinctID == "" {
		validationError(w, "distinct_id", "Either distinct_id or person_id must be provided")
		return
	}
	result, err := evaluateFlag(flag, personProperties(distinctID, nil))
	if err != nil {
		writeStatus(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}
	value := any(result.enabled)
	if result.variant != "" {
		value = result.variant
	}
	write(w, map[string]any{
		"flag_key": flag["key"], "result": value, "reason": result.reason,
		"reason_description": result.reason, "condition_index": result.conditionIndex,
		"payload": result.payload, "evaluation_distinct_id": distinctID, "conditions": []any{},
	})
}

type evaluation struct {
	enabled        bool
	variant        string
	reason         string
	conditionIndex any
	payload        any
}

// evaluateFlag is a deliberately small evaluator. It fails a flag it cannot
// evaluate exactly (other operators, partial rollouts) so tests cannot pass on
// an approximation of PostHog's matching.
func evaluateFlag(flag map[string]any, props map[string]any) (evaluation, error) {
	filters := asObject(flag["filters"])
	reason := "no_condition_match"
	for i, item := range asList(filters["groups"]) {
		group := asObject(item)
		matched, err := matchProperties(asList(group["properties"]), props)
		if err != nil {
			return evaluation{}, err
		}
		if !matched {
			continue
		}
		rollout := group["rollout_percentage"]
		inRollout := rollout == nil || toInt(rollout) == 100
		if !inRollout && toInt(rollout) != 0 {
			return evaluation{}, fmt.Errorf("mock evaluator does not support rollout %v", rollout)
		}
		if !inRollout {
			reason = "out_of_rollout_bound"
			if filters["early_exit"] == true {
				return evaluation{reason: reason, conditionIndex: i}, nil
			}
			continue
		}
		result := evaluation{enabled: true, reason: "condition_match", conditionIndex: i}
		payloadKey := "true"
		if variants := asList(asObject(filters["multivariate"])["variants"]); len(variants) > 0 {
			variant, err := pickVariant(group, variants)
			if err != nil {
				return evaluation{}, err
			}
			result.variant = variant
			payloadKey = variant
		}
		result.payload = asObject(filters["payloads"])[payloadKey]
		return result, nil
	}
	return evaluation{reason: reason}, nil
}

func pickVariant(group map[string]any, variants []any) (string, error) {
	if override, ok := group["variant"].(string); ok && override != "" {
		return override, nil
	}
	for _, v := range variants {
		if toInt(asObject(v)["rollout_percentage"]) == 100 {
			return fmt.Sprint(asObject(v)["key"]), nil
		}
	}
	return "", fmt.Errorf("mock evaluator needs a variant override or a 100%% variant")
}

func matchProperties(filters []any, props map[string]any) (bool, error) {
	for _, item := range filters {
		filter := asObject(item)
		if filter["type"] != "person" {
			return false, fmt.Errorf("mock evaluator does not support %v properties", filter["type"])
		}
		actual, present := props[fmt.Sprint(filter["key"])]
		var ok bool
		switch filter["operator"] {
		case "exact", nil:
			ok = present && valueIn(actual, filter["value"])
		case "is_not":
			ok = present && !valueIn(actual, filter["value"])
		case "icontains":
			ok = present && strings.Contains(strings.ToLower(fmt.Sprint(actual)), strings.ToLower(fmt.Sprint(filter["value"])))
		case "is_set":
			ok = present
		case "is_not_set":
			ok = !present
		default:
			return false, fmt.Errorf("mock evaluator does not support operator %v", filter["operator"])
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func valueIn(actual, expected any) bool {
	candidates, isList := expected.([]any)
	if !isList {
		candidates = []any{expected}
	}
	for _, c := range candidates {
		if strings.EqualFold(fmt.Sprint(c), fmt.Sprint(actual)) {
			return true
		}
	}
	return false
}

// personProperties merges request overrides over the stored fixture person.
func personProperties(distinctID string, overrides map[string]any) map[string]any {
	props := map[string]any{}
	if distinctID == "user_123" {
		for k, v := range asObject(person()["properties"]) {
			props[k] = v
		}
	}
	for k, v := range overrides {
		props[k] = v
	}
	return props
}

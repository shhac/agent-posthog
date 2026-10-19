package mockposthog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Fixture projects. 123 is the default; 124 stands in for a separate staging
// project; copying to 125 is gated by an approval policy.
const (
	approvalProject = 125
	approvalFlagKey = "approval-flag"
	policyFlagKey   = "policy-flag"
)

var projectTokens = map[int]string{123: "phc_mock_project_123", 124: "phc_mock_project_124"}

// flagStore is a stateful, per-server feature flag store that mirrors the
// PostHog behaviors the write commands depend on: shallow filter merges,
// version + original_flag conflict detection, archive/delete rules, approval
// policies, strict rollout updates, and evaluation.
type flagStore struct {
	mu     sync.Mutex
	nextID int
	flags  map[int]map[int]map[string]any
}

func newFlagStore() *flagStore {
	s := &flagStore{nextID: 100, flags: map[int]map[int]map[string]any{123: {}, 124: {}, approvalProject: {}}}
	for _, flag := range []map[string]any{checkoutFlag(), billingFlag(23), billingFlag(24), approvalFlag(), policyFlag()} {
		s.flags[123][toInt(flag["id"])] = withFlagDefaults(flag)
	}
	return s
}

var flagPath = regexp.MustCompile(`^/api/projects/(\d+)/feature_flags/(?:(\d+)/)?(?:([a-z_]+)/)?$`)

func (s *flagStore) handle(w http.ResponseWriter, r *http.Request) bool {
	m := flagPath.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return false
	}
	project, _ := strconv.Atoi(m[1])
	id, _ := strconv.Atoi(m[2])
	action := m[3]
	s.mu.Lock()
	defer s.mu.Unlock()
	flags, ok := s.flags[project]
	if !ok {
		writeStatus(w, http.StatusNotFound, map[string]any{"detail": "Project not found."})
		return true
	}
	switch {
	case id == 0 && action == "" && r.Method == http.MethodGet:
		s.list(w, r, flags)
	case id == 0 && action == "" && r.Method == http.MethodPost:
		s.create(w, r, project)
	case id == 0 && action == "user_blast_radius" && r.Method == http.MethodPost:
		blastRadius(w, r)
	case id != 0:
		flag, ok := flags[id]
		restoring := r.Method == http.MethodPatch && action == ""
		if !ok || flag["deleted"] == true && !restoring {
			writeStatus(w, http.StatusNotFound, map[string]any{"detail": "Not found."})
			return true
		}
		s.handleFlag(w, r, project, flag, action)
	default:
		return false
	}
	return true
}

func (s *flagStore) handleFlag(w http.ResponseWriter, r *http.Request, project int, flag map[string]any, action string) {
	switch {
	case action == "" && r.Method == http.MethodGet:
		write(w, flag)
	case action == "" && r.Method == http.MethodPatch:
		s.patch(w, r, flag)
	case action == "" && r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusMethodNotAllowed)
	case action == "set_release_condition_rollout" && r.Method == http.MethodPost:
		setRollout(w, r, flag)
	case action == "test_evaluation" && r.Method == http.MethodPost:
		testEvaluation(w, r, flag)
	case action == "dependent_flags" && r.Method == http.MethodGet:
		writePage(w, r, []any{map[string]any{"id": 25, "key": "checkout-v2-dependent", "name": "Checkout dependent", "active": true}})
	case action == "activity" && r.Method == http.MethodGet:
		writePage(w, r, flagActivity())
	default:
		writeStatus(w, http.StatusNotFound, map[string]any{"detail": "No mock route for " + r.Method + " " + r.URL.Path})
	}
}

func (s *flagStore) list(w http.ResponseWriter, r *http.Request, flags map[int]map[string]any) {
	q := r.URL.Query()
	wantArchived := q.Get("archived") == "true"
	out := []any{}
	for _, id := range sortedIDs(flags) {
		flag := flags[id]
		key := fmt.Sprint(flag["key"])
		switch {
		case flag["deleted"] == true:
		case (flag["archived"] == true) != wantArchived:
		case q.Get("key") != "" && key != q.Get("key"):
		case q.Get("search") != "" && !strings.Contains(key, q.Get("search")):
		default:
			out = append(out, flag)
		}
	}
	writePage(w, r, out)
}

func (s *flagStore) create(w http.ResponseWriter, r *http.Request, project int) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	key, _ := body["key"].(string)
	if key == "" {
		validationError(w, "key", "This field is required.")
		return
	}
	for _, existing := range s.flags[project] {
		if existing["key"] == key && existing["deleted"] != true {
			validationError(w, "key", "There is already a feature flag with this key.")
			return
		}
	}
	if fv, ok := asObject(body["filters"])["version"]; ok && fv != nil {
		validationError(w, "filters", "filters.version is reserved. Remove it from the request.")
		return
	}
	s.nextID++
	flag := withFlagDefaults(body)
	flag["id"] = s.nextID
	flag["version"] = 1
	if _, ok := body["active"]; !ok {
		flag["active"] = true
	}
	s.flags[project][s.nextID] = flag
	writeStatus(w, http.StatusCreated, flag)
}

func (s *flagStore) patch(w http.ResponseWriter, r *http.Request, flag map[string]any) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	switch flag["key"] {
	case approvalFlagKey:
		writeStatus(w, http.StatusConflict, map[string]any{
			"code": "approval_required", "status": "approval_required",
			"detail": "Changes to this flag require approval.", "message": "Changes to this flag require approval.",
			"resource_type": "feature_flag", "resource_id": fmt.Sprint(flag["id"]),
			"change_request_id": "cr_1", "change_request": map[string]any{"id": "cr_1"}, "required_approvers": []any{},
		})
		return
	case policyFlagKey:
		writeStatus(w, http.StatusBadRequest, map[string]any{
			"code": "policy_conflict", "error": "This change matches more than one approval policy.",
			"conflicting_policies": []any{}, "guidance": "Change the rollout and the targeting separately.",
		})
		return
	}
	if fv, ok := asObject(body["filters"])["version"]; ok && fv != nil {
		validationError(w, "filters", "filters.version is reserved. Remove it from the request.")
		return
	}
	original := asObject(body["original_flag"])
	sentVersion, hasVersion := body["version"]
	delete(body, "original_flag")
	delete(body, "version")

	updated := cloneMap(flag)
	for k, v := range body {
		if k == "filters" {
			merged := cloneMap(asObject(flag["filters"]))
			for fk, fv := range asObject(v) {
				merged[fk] = fv
			}
			updated["filters"] = merged
			continue
		}
		updated[k] = v
	}
	if hasVersion && toInt(sentVersion) != toInt(flag["version"]) && len(original) > 0 {
		for k := range body {
			ov, inOriginal := original[k]
			if inOriginal && !jsonEqual(updated[k], ov) && !jsonEqual(flag[k], ov) {
				writeStatus(w, http.StatusConflict, map[string]any{
					"type": "server_error", "code": "conflict",
					"detail": "The feature flag was updated by agent@example.com since you started editing it. Please refresh and try again.",
				})
				return
			}
		}
	}
	if updated["archived"] == true && updated["active"] == true {
		validationError(w, "archived", "Disable the feature flag before archiving it.")
		return
	}
	if jsonEqual(updated, flag) {
		write(w, flag)
		return
	}
	updated["version"] = toInt(flag["version"]) + 1
	for k := range flag {
		delete(flag, k)
	}
	for k, v := range updated {
		flag[k] = v
	}
	write(w, flag)
}

func setRollout(w http.ResponseWriter, r *http.Request, flag map[string]any) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	if toInt(body["version"]) != toInt(flag["version"]) {
		writeStatus(w, http.StatusConflict, map[string]any{
			"type": "server_error", "code": "conflict",
			"detail": fmt.Sprintf("This feature flag has changed since version %v; it is now at version %v. Nothing was written.", body["version"], flag["version"]),
		})
		return
	}
	groups := asList(asObject(flag["filters"])["groups"])
	index := toInt(body["condition_index"])
	if index < 0 || index >= len(groups) {
		validationError(w, "condition_index", "The flag has no release condition at that index.")
		return
	}
	asObject(groups[index])["rollout_percentage"] = body["rollout_percentage"]
	flag["version"] = toInt(flag["version"]) + 1
	write(w, flag)
}

func blastRadius(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	condition, present := body["condition"]
	if !present {
		validationError(w, "condition", "Missing condition for which to get blast radius")
		return
	}
	affected := 100
	if len(asList(asObject(condition)["properties"])) > 0 {
		affected = 25
	}
	write(w, map[string]any{"affected": affected, "total": 100})
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

func (s *flagStore) copyFlags(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key, _ := body["feature_flag_key"].(string)
	var source map[string]any
	for _, flag := range s.flags[toInt(body["from_project"])] {
		if flag["key"] == key && flag["deleted"] != true {
			source = flag
		}
	}
	if source == nil {
		writeStatus(w, http.StatusBadRequest, map[string]any{"error": "Feature flag to copy does not exist."})
		return
	}
	success, failed := []any{}, []any{}
	for _, target := range asList(body["target_project_ids"]) {
		project := toInt(target)
		if project == approvalProject {
			failed = append(failed, map[string]any{"project_id": project, "error_message": "Approval required.", "approval_pending": true, "change_request_id": "cr_copy"})
			continue
		}
		flags, ok := s.flags[project]
		if !ok {
			failed = append(failed, map[string]any{"project_id": project, "error_message": "Project not found."})
			continue
		}
		var existing map[string]any
		for _, flag := range flags {
			if flag["key"] == key && flag["deleted"] != true {
				existing = flag
			}
		}
		active := source["active"] == true && body["disable_copied_flag"] != true
		if existing == nil {
			s.nextID++
			existing = withFlagDefaults(map[string]any{"id": s.nextID, "key": key, "version": 0})
			flags[s.nextID] = existing
		}
		existing["name"] = source["name"]
		existing["filters"] = cloneMap(asObject(source["filters"]))
		existing["active"] = active
		existing["is_remote_configuration"] = source["is_remote_configuration"]
		existing["version"] = toInt(existing["version"]) + 1
		success = append(success, map[string]any{
			"id": existing["id"], "key": key, "name": existing["name"], "active": active,
			"team_id": project, "updated_existing": toInt(existing["version"]) > 1,
		})
	}
	write(w, map[string]any{"success": success, "failed": failed})
}

func dependencyRequirements(w http.ResponseWriter) {
	write(w, map[string]any{
		"can_copy_dependencies": true, "dependency_count": 0, "copied_dependency_keys": []any{},
		"reused_dependency_keys": []any{}, "warnings": []any{}, "reason": "",
	})
}

func approvalFlag() map[string]any {
	return map[string]any{"id": 26, "key": approvalFlagKey, "name": "Approval gated", "active": false, "version": 1}
}

func policyFlag() map[string]any {
	return map[string]any{"id": 27, "key": policyFlagKey, "name": "Policy conflicted", "active": false, "version": 1}
}

func withFlagDefaults(flag map[string]any) map[string]any {
	defaults := map[string]any{
		"name": "", "active": false, "deleted": false, "archived": false, "version": 1,
		"is_remote_configuration": false, "has_encrypted_payloads": false,
		"tags": []any{}, "evaluation_contexts": []any{},
		"filters": map[string]any{"groups": []any{}},
	}
	out := normalize(flag)
	for k, v := range defaults {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

func decodeBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		validationError(w, "body", "Invalid JSON body.")
		return nil, false
	}
	return body, true
}

func validationError(w http.ResponseWriter, attr, detail string) {
	writeStatus(w, http.StatusBadRequest, map[string]any{"type": "validation_error", "code": "invalid_input", "attr": attr, "detail": detail})
}

// normalize round-trips through JSON so fixtures built from Go literals compare
// and mutate exactly like decoded request bodies.
func normalize(v map[string]any) map[string]any {
	data, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	return out
}

func cloneMap(v map[string]any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	return normalize(v)
}

func jsonEqual(a, b any) bool {
	ad, _ := json.Marshal(a)
	bd, _ := json.Marshal(b)
	return bytes.Equal(ad, bd)
}

func asObject(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	default:
		n, _ := strconv.Atoi(fmt.Sprint(v))
		return n
	}
}

func sortedIDs(flags map[int]map[string]any) []int {
	ids := make([]int, 0, len(flags))
	for id := range flags {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

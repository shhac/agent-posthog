package mockposthog

import (
	"fmt"
	"maps"
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
	approvalProject     = 125
	tagsRequiredProject = 126
	approvalFlagKey     = "approval-flag"
	policyFlagKey       = "policy-flag"
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
	s := &flagStore{nextID: 100, flags: map[int]map[int]map[string]any{123: {}, 124: {}, approvalProject: {}, tagsRequiredProject: {}}}
	for _, flag := range []map[string]any{checkoutFlag(), billingFlag(23), billingFlag(24), approvalFlag(), policyFlag(), encryptedFlag(), v2Flag(), legacyFlag()} {
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
		writePage(w, r, s.dependents(project, flag))
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
	if findByKey(s.flags[project], key) != nil {
		validationError(w, "key", "There is already a feature flag with this key.")
		return
	}
	if project == tagsRequiredProject && len(asList(body["tags"])) == 0 {
		validationError(w, "tags", "This project requires at least one tag on new feature flags.")
		return
	}
	if isV2(body) && body["active"] == true {
		validationError(w, "active", "A flag with this configuration format is created disabled. Enable it in a separate request.")
		return
	}
	if fv, ok := asObject(body["filters"])["version"]; ok && fv != nil && !isV2(body) {
		validationError(w, "filters", "filters.version is reserved. Remove it from the request.")
		return
	}
	s.nextID++
	flag := withFlagDefaults(body)
	storeNullDefaults(flag)
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
	if isV2(flag) {
		s.patchV2(w, body, flag)
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
	storeNullDefaults(updated)
	if updated["archived"] == true && updated["active"] == true {
		validationError(w, "archived", "Disable the feature flag before archiving it.")
		return
	}
	if jsonEqual(updated, flag) {
		write(w, flag)
		return
	}
	updated["version"] = toInt(flag["version"]) + 1
	clear(flag)
	maps.Copy(flag, updated)
	write(w, flag)
}

// patchV2 mirrors PostHog's config version 2 path: a strict version check and
// whole-document filters, with the classic shallow merge unavailable.
func (s *flagStore) patchV2(w http.ResponseWriter, body, flag map[string]any) {
	if sent, ok := body["version"]; ok && toInt(sent) != toInt(flag["version"]) {
		writeStatus(w, http.StatusConflict, map[string]any{"type": "server_error", "code": "conflict", "detail": "This feature flag has changed since the version you sent."})
		return
	}
	delete(body, "version")
	delete(body, "original_flag")
	if filters, ok := body["filters"]; ok && !isV2(map[string]any{"filters": filters}) {
		validationError(w, "filters", "This flag's stored configuration cannot be updated through this API.")
		return
	}
	for k, v := range body {
		flag[k] = v
	}
	flag["version"] = toInt(flag["version"]) + 1
	write(w, flag)
}

// storeNullDefaults fills condition keys PostHog writes as null when a
// request leaves them out.
func storeNullDefaults(flag map[string]any) {
	for _, group := range asList(asObject(flag["filters"])["groups"]) {
		g := asObject(group)
		if _, ok := g["aggregation_group_type_index"]; !ok {
			g["aggregation_group_type_index"] = nil
		}
	}
}

// dependents lists flags whose conditions depend on flag (plus the legacy
// fixture dependent of checkout-v2).
func (s *flagStore) dependents(project int, flag map[string]any) []any {
	out := []any{}
	if toInt(flag["id"]) == 22 {
		out = append(out, map[string]any{"id": 25, "key": "checkout-v2-dependent", "name": "Checkout dependent", "active": true})
	}
	for _, id := range sortedIDs(s.flags[project]) {
		other := s.flags[project][id]
		if other["deleted"] == true {
			continue
		}
		for _, group := range asList(asObject(other["filters"])["groups"]) {
			for _, prop := range asList(asObject(group)["properties"]) {
				p := asObject(prop)
				if p["type"] == "flag" && fmt.Sprint(p["key"]) == fmt.Sprint(flag["id"]) {
					out = append(out, map[string]any{"id": other["id"], "key": other["key"], "active": other["active"]})
				}
			}
		}
	}
	return out
}

func hasFlagDependency(flag map[string]any) bool {
	for _, group := range asList(asObject(flag["filters"])["groups"]) {
		for _, prop := range asList(asObject(group)["properties"]) {
			if asObject(prop)["type"] == "flag" {
				return true
			}
		}
	}
	return false
}

func isV2(flag map[string]any) bool {
	return toInt(asObject(flag["filters"])["version"]) == 2
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

func (s *flagStore) copyFlags(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key, _ := body["feature_flag_key"].(string)
	source := findByKey(s.flags[toInt(body["from_project"])], key)
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
		existing := findByKey(flags, key)
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
		item := map[string]any{
			"id": existing["id"], "key": key, "name": existing["name"], "active": active,
			"team_id": project, "updated_existing": toInt(existing["version"]) > 1,
		}
		if body["copy_dependencies"] != true && hasFlagDependency(source) {
			item["flag_dependency_warnings"] = []any{"Dropped a dependency on a flag the target project lacks; the copy is disabled."}
			item["active"], existing["active"] = false, false
		}
		if body["copy_schedule"] == true {
			item["schedule_copy_warning"] = "Existing schedules in the target project were kept."
		}
		success = append(success, item)
	}
	write(w, map[string]any{"success": success, "failed": failed})
}

func dependencyRequirements(w http.ResponseWriter) {
	write(w, map[string]any{
		"can_copy_dependencies": true, "dependency_count": 0, "copied_dependency_keys": []any{},
		"reused_dependency_keys": []any{}, "warnings": []any{}, "reason": "",
	})
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

// findByKey returns the live flag with key, picking the lowest ID so
// duplicate-key fixtures resolve the same way every time.
func findByKey(flags map[int]map[string]any, key string) map[string]any {
	for _, id := range sortedIDs(flags) {
		if flags[id]["key"] == key && flags[id]["deleted"] != true {
			return flags[id]
		}
	}
	return nil
}

func sortedIDs(flags map[int]map[string]any) []int {
	ids := make([]int, 0, len(flags))
	for id := range flags {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

package mockposthog

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

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
		notFound(w, "Project not found.")
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
			notFound(w, "Not found.")
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
		notFound(w, "No mock route for "+r.Method+" "+r.URL.Path)
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

package mockposthog

import (
	"fmt"
	"maps"
	"net/http"
)

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

	updated := mergeFlagPatch(flag, body)
	if hasVersion && toInt(sentVersion) != toInt(flag["version"]) && staleConflict(flag, updated, original, body) {
		conflict(w, "The feature flag was updated by agent@example.com since you started editing it. Please refresh and try again.")
		return
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

// mergeFlagPatch applies a PATCH body the way PostHog does: fields replace,
// filters merge one level deep.
func mergeFlagPatch(flag, body map[string]any) map[string]any {
	updated := cloneMap(flag)
	for k, v := range body {
		if k != "filters" {
			updated[k] = v
			continue
		}
		merged := cloneMap(asObject(flag["filters"]))
		maps.Copy(merged, asObject(v))
		updated["filters"] = merged
	}
	return updated
}

// staleConflict mirrors PostHog's field-level check: a stale write conflicts
// only on a field the caller changed that someone else changed differently.
func staleConflict(flag, updated, original, body map[string]any) bool {
	if len(original) == 0 {
		return false
	}
	for k := range body {
		ov, inOriginal := original[k]
		if inOriginal && !jsonEqual(updated[k], ov) && !jsonEqual(flag[k], ov) {
			return true
		}
	}
	return false
}

// patchV2 mirrors PostHog's config version 2 path: a strict version check and
// whole-document filters, with the classic shallow merge unavailable.
func (s *flagStore) patchV2(w http.ResponseWriter, body, flag map[string]any) {
	if sent, ok := body["version"]; ok && toInt(sent) != toInt(flag["version"]) {
		conflict(w, "This feature flag has changed since the version you sent.")
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

func isV2(flag map[string]any) bool {
	return toInt(asObject(flag["filters"])["version"]) == 2
}

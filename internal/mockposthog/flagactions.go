package mockposthog

import (
	"fmt"
	"net/http"
)

func setRollout(w http.ResponseWriter, r *http.Request, flag map[string]any) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}
	if toInt(body["version"]) != toInt(flag["version"]) {
		conflict(w, fmt.Sprintf("This feature flag has changed since version %v; it is now at version %v. Nothing was written.", body["version"], flag["version"]))
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

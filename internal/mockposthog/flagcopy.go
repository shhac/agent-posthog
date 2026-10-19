package mockposthog

import "net/http"

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

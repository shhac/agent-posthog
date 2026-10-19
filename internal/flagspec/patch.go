package flagspec

import (
	"maps"
	"slices"
)

// Patch is a minimal write: top-level flag fields plus the filters sections
// that change. PostHog merges filters shallowly, so only sections present here
// are replaced; a nil section value clears it.
type Patch struct {
	Fields  map[string]any
	Filters map[string]any
}

func NewPatch() *Patch {
	return &Patch{Fields: map[string]any{}, Filters: map[string]any{}}
}

// Empty reports whether the patch changes nothing.
func (p *Patch) Empty() bool { return len(p.Fields) == 0 && len(p.Filters) == 0 }

// Body renders the request body.
func (p *Patch) Body() map[string]any {
	body := make(map[string]any, len(p.Fields)+1)
	for k, v := range p.Fields {
		body[k] = v
	}
	if len(p.Filters) > 0 {
		body["filters"] = p.Filters
	}
	return body
}

// Prune drops patch entries that already match current, so plans list only real
// changes and unchanged writes are skipped.
func (p *Patch) Prune(current map[string]any) {
	if current == nil {
		return
	}
	for key, value := range p.Fields {
		if existing, ok := current[key]; ok && Equal(existing, value) {
			delete(p.Fields, key)
		}
	}
	filters := Object(current["filters"])
	for key, value := range p.Filters {
		existing, ok := filters[key]
		if !ok && value == nil {
			delete(p.Filters, key)
			continue
		}
		if key == "groups" {
			existing, value = normalizeGroups(existing), normalizeGroups(value)
		}
		if Equal(existing, value) {
			delete(p.Filters, key)
		}
	}
}

// normalizeGroups treats null defaults like missing keys; they evaluate the same.
func normalizeGroups(v any) any {
	groups := List(Clone(v))
	for _, item := range groups {
		group := Object(item)
		for _, key := range nullAsAbsentGroupKeys {
			if value, ok := group[key]; ok && value == nil {
				delete(group, key)
			}
		}
	}
	if groups == nil {
		return v
	}
	return groups
}

// nullAsAbsentGroupKeys are condition keys PostHog stores as null by default,
// so a null and a missing key describe the same condition.
var nullAsAbsentGroupKeys = []string{"variant", "aggregation_group_type_index"}

// Result is the flag as it would be after the patch: fields replaced, filter
// sections merged shallowly (a nil section cleared), as PostHog applies it.
func Result(current map[string]any, p *Patch) map[string]any {
	result, _ := Clone(current).(map[string]any)
	if result == nil {
		result = map[string]any{}
	}
	for key, value := range p.Fields {
		result[key] = Clone(value)
	}
	filters, _ := Clone(Object(result["filters"])).(map[string]any)
	if filters == nil {
		filters = map[string]any{}
	}
	for key, value := range p.Filters {
		if value == nil {
			delete(filters, key)
			continue
		}
		filters[key] = Clone(value)
	}
	result["filters"] = filters
	return result
}

// Unapplied lists the patch entries the stored flag does not reflect, which
// happens when PostHog normalizes or drops part of a write.
func Unapplied(p *Patch, stored map[string]any) []string {
	var paths []string
	for _, key := range slices.Sorted(maps.Keys(p.Fields)) {
		if !Equal(stored[key], p.Fields[key]) {
			paths = append(paths, key)
		}
	}
	filters := Object(stored["filters"])
	for _, key := range slices.Sorted(maps.Keys(p.Filters)) {
		sent, got := p.Filters[key], filters[key]
		if key == "groups" {
			sent, got = normalizeGroups(sent), normalizeGroups(got)
		}
		if !Equal(got, sent) {
			paths = append(paths, "filters."+key)
		}
	}
	return paths
}

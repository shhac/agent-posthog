package flagspec

import (
	"iter"
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
	for e := range p.entries(current) {
		switch {
		case e.field && e.present && Equal(e.current, e.value):
			delete(p.Fields, e.key)
		case !e.field && Equal(e.current, e.value):
			delete(p.Filters, e.key)
		}
	}
}

// patchEntry is one patch entry beside what a flag holds for it. Condition
// groups are normalized on both sides, so null defaults compare as absent.
type patchEntry struct {
	// field is true for a top-level field, false for a filters section.
	field   bool
	key     string
	path    string
	current any
	present bool
	value   any
}

// entries walks the patch in path order: fields first, then filter sections.
func (p *Patch) entries(flag map[string]any) iter.Seq[patchEntry] {
	return func(yield func(patchEntry) bool) {
		for _, key := range slices.Sorted(maps.Keys(p.Fields)) {
			current, present := flag[key]
			if !yield(patchEntry{field: true, key: key, path: key, current: current, present: present, value: p.Fields[key]}) {
				return
			}
		}
		filters := Filters(flag)
		for _, key := range slices.Sorted(maps.Keys(p.Filters)) {
			current, present := filters[key]
			value := p.Filters[key]
			if key == "groups" {
				current, value = normalizeGroups(current), normalizeGroups(value)
			}
			if !yield(patchEntry{key: key, path: "filters." + key, current: current, present: present, value: value}) {
				return
			}
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
	filters, _ := Clone(Filters(result)).(map[string]any)
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
	for e := range p.entries(stored) {
		if !Equal(e.current, e.value) {
			paths = append(paths, e.path)
		}
	}
	return paths
}

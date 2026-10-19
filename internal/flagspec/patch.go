package flagspec

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
	filters := asMap(current["filters"])
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

// normalizeGroups treats a null variant like a missing one; they evaluate the same.
func normalizeGroups(v any) any {
	groups := asSlice(Clone(v))
	for _, item := range groups {
		group := asMap(item)
		if variant, ok := group["variant"]; ok && variant == nil {
			delete(group, "variant")
		}
	}
	if groups == nil {
		return v
	}
	return groups
}

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
	filters, _ := Clone(asMap(result["filters"])).(map[string]any)
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

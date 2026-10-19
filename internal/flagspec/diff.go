package flagspec

import (
	"fmt"
	"sort"
)

// Absent marks a side of a change where the field does not exist, as opposed
// to existing with a null value.
const Absent = "(absent)"

// Change is one leaf-level difference between two flag definitions.
type Change struct {
	Path   string `json:"path"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// Changes lists what applying patch to current would change.
func Changes(current map[string]any, p *Patch) []Change {
	if current == nil {
		current = map[string]any{}
	}
	var out []Change
	for _, key := range sortedKeys(p.Fields) {
		before, ok := current[key]
		out = append(out, diffValues(key, presence(before, ok), p.Fields[key])...)
	}
	filters := asMap(current["filters"])
	for _, key := range sortedKeys(p.Filters) {
		before, ok := filters[key]
		after := p.Filters[key]
		if key == "groups" {
			before, after = normalizeGroups(before), normalizeGroups(after)
		}
		out = append(out, diffValues("filters."+key, presence(before, ok), after)...)
	}
	return out
}

// Diff lists the differences between two decoded JSON values.
func Diff(path string, before, after any) []Change {
	return diffValues(path, before, after)
}

func diffValues(path string, before, after any) []Change {
	if Equal(before, after) {
		return nil
	}
	bm, bok := before.(map[string]any)
	am, aok := after.(map[string]any)
	if bok && aok {
		keys := map[string]bool{}
		for k := range bm {
			keys[k] = true
		}
		for k := range am {
			keys[k] = true
		}
		var out []Change
		for _, k := range sortedSet(keys) {
			bv, bin := bm[k]
			av, ain := am[k]
			out = append(out, diffValues(path+"."+k, presence(bv, bin), presence(av, ain))...)
		}
		return out
	}
	bs, bok := before.([]any)
	as, aok := after.([]any)
	if bok && aok && len(bs) == len(as) {
		var out []Change
		for i := range bs {
			out = append(out, diffValues(fmt.Sprintf("%s[%d]", path, i), bs[i], as[i])...)
		}
		return out
	}
	return []Change{{Path: path, Before: before, After: after}}
}

func presence(v any, ok bool) any {
	if !ok {
		return Absent
	}
	return v
}

func sortedSet(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

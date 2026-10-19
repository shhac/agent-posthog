package flagspec

import (
	"fmt"
	"maps"
	"slices"
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
	var out []Change
	for e := range p.entries(current) {
		out = append(out, Diff(e.path, presence(e.current, e.present), e.value)...)
	}
	return out
}

// Diff lists the differences between two decoded JSON values; an empty path
// names top-level keys bare.
func Diff(path string, before, after any) []Change {
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
		for _, k := range slices.Sorted(maps.Keys(keys)) {
			bv, bin := bm[k]
			av, ain := am[k]
			out = append(out, Diff(joinPath(path, k), presence(bv, bin), presence(av, ain))...)
		}
		return out
	}
	bs, bok := before.([]any)
	as, aok := after.([]any)
	if bok && aok && len(bs) == len(as) {
		var out []Change
		for i := range bs {
			out = append(out, Diff(fmt.Sprintf("%s[%d]", path, i), bs[i], as[i])...)
		}
		return out
	}
	return []Change{{Path: path, Before: before, After: after}}
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func presence(v any, ok bool) any {
	if !ok {
		return Absent
	}
	return v
}

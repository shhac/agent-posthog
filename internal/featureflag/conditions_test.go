package featureflag

import (
	"context"
	"testing"
)

func TestMoveGroupLandsAtFinalIndex(t *testing.T) {
	cases := []struct {
		from, to int
		want     string
	}{
		{0, 3, "bcda"}, {3, 0, "dabc"}, {1, 2, "acbd"}, {2, 1, "acbd"}, {2, 2, "abcd"},
	}
	for _, tc := range cases {
		groups := []any{"a", "b", "c", "d"}
		got, err := moveGroup(groups, tc.from, tc.to)
		if err != nil {
			t.Fatal(err)
		}
		joined := ""
		for _, g := range got {
			joined += g.(string)
		}
		if joined != tc.want {
			t.Fatalf("move %d→%d = %s, want %s", tc.from, tc.to, joined, tc.want)
		}
	}
	if _, err := moveGroup([]any{"a"}, 0, 1); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func TestPositionalEditsRefuseOutOfRangeIndexes(t *testing.T) {
	flag := map[string]any{"key": "k", "filters": map[string]any{"groups": []any{
		map[string]any{"properties": []any{}, "rollout_percentage": 100},
	}}}
	p := Project{}
	rollout := 50.0
	for name, build := range map[string]Build{
		"remove -1":   RemoveCondition(-1),
		"remove 1":    RemoveCondition(1),
		"edit -1":     EditCondition(-1, ConditionEdit{Rollout: &rollout}),
		"move -1 → 0": MoveCondition(-1, 0),
		"move 0 → -1": MoveCondition(0, -1),
	} {
		if _, _, err := build(context.Background(), p, flag); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

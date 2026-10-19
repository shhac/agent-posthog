package featureflag

import "testing"

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

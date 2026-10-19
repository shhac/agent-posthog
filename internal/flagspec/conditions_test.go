package flagspec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConditionWarnings(t *testing.T) {
	groups := []any{
		map[string]any{"properties": []any{}, "rollout_percentage": json.Number("100")},
		map[string]any{"properties": []any{map[string]any{"key": "email"}}, "rollout_percentage": json.Number("100")},
	}
	if got := ConditionWarnings(groups, TypeBoolean); len(got) != 1 || !strings.Contains(got[0], "Condition 0") {
		t.Fatalf("warnings = %v", got)
	}
	if got := ConditionWarnings(nil, TypeBoolean); len(got) != 1 || !strings.Contains(got[0], "matches nobody") {
		t.Fatalf("warnings = %v", got)
	}
	if got := ConditionWarnings(groups[1:], TypeBoolean); len(got) != 0 {
		t.Fatalf("warnings = %v", got)
	}
}

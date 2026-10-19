package flagspec

import "testing"

func TestResultMergesPatchShallowly(t *testing.T) {
	flag := mustFlag(t, multivariateFlag)
	p := NewPatch()
	p.Fields["is_remote_configuration"] = true
	p.Filters["multivariate"] = nil
	p.Filters["early_exit"] = true
	result := Result(flag, p)
	filters := Object(result["filters"])
	if _, ok := filters["multivariate"]; ok || filters["early_exit"] != true || filters["groups"] == nil {
		t.Fatalf("filters = %v", filters)
	}
	if TypeOf(result) != TypeRemoteConfig || TypeOf(flag) != TypeMultivariate {
		t.Fatal("Result must not alias or keep the old type")
	}
}

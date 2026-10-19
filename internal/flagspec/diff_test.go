package flagspec

import (
	"encoding/json"
	"testing"
)

func TestChangesReportPaths(t *testing.T) {
	flag := mustFlag(t, boolFlag)
	patch, err := buildFrom(t, flag, `{"key":"beta-gate","active":false,"conditions":[{"where":["email icontains @contractor.com"],"rollout":0},{"where":[{"key":"$browser","type":"event","value":"Chrome","operator":"exact"}],"rollout":50,"sort_key":"abc"},{"rollout":null}]}`)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(Changes(flag, patch))
	want := `[{"path":"active","before":true,"after":false},{"path":"filters.groups[1].rollout_percentage","before":100,"after":50}]`
	if string(got) != want {
		t.Fatalf("changes = %s\nwant      %s", got, want)
	}
}

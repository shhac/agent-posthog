package flags

import (
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

func TestCreateFlagsDescribeOneConditionOrRefuseRemoteConfigFilters(t *testing.T) {
	spec, err := createFlags{flagType: flagspec.TypeBoolean, where: []string{"plan = pro"}, rollout: 25, tags: []string{"beta"}}.spec("gate", true)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Key() != "gate" || !spec.Has("conditions") || !spec.Has("tags") || spec.Has("name") {
		t.Fatalf("spec fields: conditions=%v tags=%v name=%v", spec.Has("conditions"), spec.Has("tags"), spec.Has("name"))
	}
	remote := createFlags{flagType: flagspec.TypeRemoteConfig, payloadJSON: `{"a":1}`}
	if spec, err := remote.spec("site-config", false); err != nil || spec.Has("conditions") {
		t.Fatalf("remote_config: err=%v", err)
	}
	if _, err := remote.spec("site-config", true); err == nil || !strings.Contains(err.Error(), "drop --where and --rollout") {
		t.Fatalf("err = %v", err)
	}
	if _, err := (createFlags{flagType: flagspec.TypeBoolean, payloadJSON: "{"}).spec("x", false); err == nil || !strings.Contains(err.Error(), "--payload-json") {
		t.Fatalf("err = %v", err)
	}
}

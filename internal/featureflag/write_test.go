package featureflag

import (
	"context"
	"strings"
	"testing"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

func TestPlanThenCommitAppliesAndReportsTheNewVersion(t *testing.T) {
	p := mockProject(t)
	ctx := context.Background()
	pending, err := p.Plan(ctx, "checkout-v2", "disable", NoVersion, Lifecycle("disable"))
	if err != nil {
		t.Fatal(err)
	}
	if !pending.Changed() || pending.Receipt.Outcome != OutcomePlanned {
		t.Fatalf("plan = %+v", pending.Receipt)
	}
	r, err := pending.Commit(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Outcome != OutcomeApplied || flagspec.Int(r.Version) <= flagspec.Int(r.BaseVersion) {
		t.Fatalf("receipt = %+v", r)
	}
	if flag, _ := p.Fetch(ctx, "checkout-v2"); flag["active"] != false {
		t.Fatalf("flag = %v", flag)
	}
}

func TestCommitRefusesAFlagChangedSinceThePlan(t *testing.T) {
	p := mockProject(t)
	ctx := context.Background()
	stale, err := p.Plan(ctx, "checkout-v2", "disable", NoVersion, Lifecycle("disable"))
	if err != nil {
		t.Fatal(err)
	}
	meanwhile, err := p.Plan(ctx, "checkout-v2", "archive", NoVersion, Lifecycle("archive"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meanwhile.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := stale.Commit(ctx); err == nil || !strings.Contains(err.Error(), "Not applied") {
		t.Fatalf("a write planned against an older version must be refused: %v", err)
	}
}

func TestPlanRefusesAStaleIfVersion(t *testing.T) {
	p := mockProject(t)
	ctx := context.Background()
	if _, err := p.Plan(ctx, "checkout-v2", "disable", 999, Lifecycle("disable")); err == nil || !strings.Contains(err.Error(), "not 999") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlanApplyCreatesOrUpdatesByKey(t *testing.T) {
	p := mockProject(t)
	ctx := context.Background()
	spec, err := flagspec.ParseSpec([]byte(`{"key":"brand-new","conditions":[{}]}`))
	if err != nil {
		t.Fatal(err)
	}
	created, err := p.PlanApply(ctx, spec, NoVersion)
	if err != nil || created.Receipt.Operation != "create" {
		t.Fatalf("receipt=%+v err=%v", created, err)
	}
	if _, err := created.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	spec, _ = flagspec.ParseSpec([]byte(`{"key":"brand-new","name":"Renamed"}`))
	updated, err := p.PlanApply(ctx, spec, NoVersion)
	if err != nil || updated.Receipt.Operation != "apply" || !updated.Changed() {
		t.Fatalf("receipt=%+v err=%v", updated, err)
	}
	if _, err := p.PlanCreate(ctx, spec); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("create must refuse a taken key: %v", err)
	}
}

func TestPlanCopyRefusesTheSourceAndTooManyTargets(t *testing.T) {
	p := mockProject(t)
	ctx := context.Background()
	if _, err := p.PlanCopy(ctx, "checkout-v2", []Target{{ID: 123}}, CopyOptions{}); err == nil || !strings.Contains(err.Error(), "source project") {
		t.Fatalf("err = %v", err)
	}
	targets := make([]Target, 0, maxCopyTargets+1)
	for id := 1000; len(targets) <= maxCopyTargets; id++ {
		targets = append(targets, Target{ID: id})
	}
	if _, err := p.PlanCopy(ctx, "checkout-v2", targets, CopyOptions{}); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("err = %v", err)
	}
}

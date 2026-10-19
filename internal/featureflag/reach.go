package featureflag

import (
	"context"
	"fmt"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

// ConditionReach is how many users one condition's filters match, before its
// rollout. Conditions overlap, so reaches do not sum.
type ConditionReach struct {
	Index       int
	Matching    any
	Total       any
	Rollout     any
	Aggregation any
}

// Reach counts the users each condition of flag matches, or only the
// condition at only when it is not negative.
func (p Project) Reach(ctx context.Context, flag map[string]any, only int) ([]ConditionReach, error) {
	filters := flagspec.Filters(flag)
	groups := flagspec.List(filters["groups"])
	var out []ConditionReach
	for i, item := range groups {
		if only >= 0 && i != only {
			continue
		}
		group := flagspec.Object(item)
		aggregation, ok := group["aggregation_group_type_index"]
		if !ok {
			aggregation = filters["aggregation_group_type_index"]
		}
		body := map[string]any{"condition": group, "group_type_index": aggregation}
		raw, err := p.Client.Post(ctx, p.flagsPath("user_blast_radius"), nil, body)
		if err != nil {
			return nil, err
		}
		counts, err := flagspec.DecodeObject(raw)
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByRetry)
		}
		out = append(out, ConditionReach{Index: i, Matching: counts["affected"], Total: counts["total"], Rollout: group["rollout_percentage"], Aggregation: aggregation})
	}
	if len(out) == 0 && only >= 0 {
		return nil, agenterrors.New(fmt.Sprintf("the flag has no condition %d", only), agenterrors.FixableByAgent)
	}
	return out, nil
}

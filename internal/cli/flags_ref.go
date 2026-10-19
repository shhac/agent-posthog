package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

func flagsPath(projectID int) string {
	return fmt.Sprintf("/api/projects/%d/feature_flags/", projectID)
}

func flagPath(projectID int, id string) string {
	return fmt.Sprintf("/api/projects/%d/feature_flags/%s/", projectID, id)
}

// resolveFlagID maps a flag reference to its numeric ID. A numeric reference is
// an ID; anything else is a key, matched exactly, falling back to archived
// flags because PostHog hides them from the default list. A flag whose key is
// itself numeric must be addressed by its ID.
func resolveFlagID(ctx context.Context, resolved *resolvedContext, idOrKey string) (string, error) {
	return resolveFlagIDIn(ctx, resolved, resolved.ProjectID, idOrKey)
}

func resolveFlagIDIn(ctx context.Context, resolved *resolvedContext, projectID int, idOrKey string) (string, error) {
	if _, err := strconv.Atoi(idOrKey); err == nil {
		return idOrKey, nil
	}
	matches, err := flagIDsByKey(ctx, resolved, projectID, idOrKey, false)
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		matches, err = flagIDsByKey(ctx, resolved, projectID, idOrKey, true)
		if err != nil {
			return "", err
		}
	}
	if len(matches) == 0 {
		return "", agenterrors.New("No feature flag matched key "+idOrKey, agenterrors.FixableByAgent).
			WithHint("Run 'agent-posthog flags list --search " + idOrKey + "' to inspect matches.")
	}
	if len(matches) > 1 {
		return "", agenterrors.New("Multiple feature flags matched key "+idOrKey, agenterrors.FixableByAgent).
			WithHint("Use the numeric feature flag ID.")
	}
	return matches[0], nil
}

func flagIDsByKey(ctx context.Context, resolved *resolvedContext, projectID int, key string, archived bool) ([]string, error) {
	q := baseValues(100)
	// key filters exactly where supported; search narrows the page if not.
	q.Set("key", key)
	q.Set("search", key)
	if archived {
		q.Set("archived", "true")
	}
	page, err := resolved.Client.List(ctx, flagsPath(projectID), q)
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, raw := range page.Results {
		var flag map[string]any
		if err := json.Unmarshal(raw, &flag); err != nil {
			continue
		}
		if fmt.Sprint(flag["key"]) == key {
			matches = append(matches, fmt.Sprint(flag["id"]))
		}
	}
	return matches, nil
}

// fetchFlag resolves and reads a flag with lossless number decoding, for
// commands that write back what they read.
func fetchFlag(ctx context.Context, resolved *resolvedContext, projectID int, ref string) (map[string]any, error) {
	id, err := resolveFlagIDIn(ctx, resolved, projectID, ref)
	if err != nil {
		return nil, err
	}
	raw, err := resolved.Client.Get(ctx, flagPath(projectID, id), nil)
	if err != nil {
		return nil, err
	}
	flag, err := flagspec.DecodeObject(raw)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("PostHog returned an unexpected feature flag shape")
	}
	return flag, nil
}

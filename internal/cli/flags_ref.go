package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

// resolveFlagID maps a flag reference to its numeric ID. "id:<n>" and
// "key:<k>" say which one is meant; a bare number is an ID and anything else a
// key, so a flag whose key is a number is reached with "key:".
func resolveFlagID(ctx context.Context, resolved *resolvedContext, idOrKey string) (string, error) {
	return resolveFlagIDIn(ctx, resolved, resolved.ProjectID, idOrKey)
}

func resolveFlagIDIn(ctx context.Context, resolved *resolvedContext, projectID int, ref string) (string, error) {
	id, key, err := parseFlagRef(ref)
	if err != nil || id != "" {
		return id, err
	}
	id, found, err := lookupFlagByKey(ctx, resolved, projectID, key)
	if err != nil {
		return "", err
	}
	if !found {
		return "", agenterrors.New("No feature flag matched key "+key, agenterrors.FixableByAgent).
			WithHint("Run 'agent-posthog flags list --search " + key + "' to inspect matches.")
	}
	return id, nil
}

// parseFlagRef splits a reference into an ID or a key.
func parseFlagRef(ref string) (id, key string, err error) {
	switch {
	case strings.HasPrefix(ref, "key:"):
		key = strings.TrimPrefix(ref, "key:")
	case strings.HasPrefix(ref, "id:"):
		id = strings.TrimPrefix(ref, "id:")
		if _, convErr := strconv.Atoi(id); convErr != nil {
			return "", "", agenterrors.New("flag reference "+ref+" needs a numeric ID after id:", agenterrors.FixableByAgent)
		}
		return id, "", nil
	default:
		if _, convErr := strconv.Atoi(ref); convErr == nil {
			return ref, "", nil
		}
		key = ref
	}
	if key == "" {
		return "", "", agenterrors.New("flag reference "+ref+" is empty", agenterrors.FixableByAgent)
	}
	return "", key, nil
}

// keyLookupPages bounds how far a key lookup pages before giving up.
var keyLookupPages = listOptions{Limit: 100, All: true, PageLimit: 20}

func flagIDsByKey(ctx context.Context, resolved *resolvedContext, projectID int, key string, archived bool) ([]string, error) {
	q := url.Values{}
	// key filters exactly where supported; search narrows the results if not.
	q.Set("key", key)
	q.Set("search", key)
	if archived {
		q.Set("archived", "true")
	}
	results, next, err := collectList(ctx, resolved, flagsPath(projectID), q, keyLookupPages)
	if err != nil {
		return nil, err
	}
	if next != "" {
		// Stopping early could miss the flag and turn an update into a duplicate create.
		return nil, agenterrors.New("too many feature flags match key "+key+" to look it up reliably", agenterrors.FixableByAgent).
			WithHint("Use the numeric feature flag ID.")
	}
	var matches []string
	for _, raw := range results {
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

// currentFlag reads the flag a command targets in the current project.
func currentFlag(ctx context.Context, resolved *resolvedContext, ref string) (map[string]any, error) {
	if err := requireProject(resolved); err != nil {
		return nil, err
	}
	return fetchFlag(ctx, resolved, resolved.ProjectID, ref)
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

// lookupFlagByKey finds a flag by key only, never treating the argument as an
// ID, so a spec whose key happens to be numeric still matches by key.
func lookupFlagByKey(ctx context.Context, resolved *resolvedContext, projectID int, key string) (string, bool, error) {
	for _, archived := range []bool{false, true} {
		ids, err := flagIDsByKey(ctx, resolved, projectID, key, archived)
		if err != nil {
			return "", false, err
		}
		switch len(ids) {
		case 0:
			continue
		case 1:
			return ids[0], true, nil
		default:
			return "", false, agenterrors.New("Multiple feature flags matched key "+key, agenterrors.FixableByAgent).
				WithHint("Use the numeric feature flag ID.")
		}
	}
	return "", false, nil
}

// flagResolver resolves flag dependency references inside --where expressions.
func flagResolver(ctx context.Context, resolved *resolvedContext, projectID int) flagspec.FlagResolver {
	return func(ref string) (string, error) {
		return resolveFlagIDIn(ctx, resolved, projectID, ref)
	}
}

// flagInProject reads the flag with key in a project, or nil if it has none.
func flagInProject(ctx context.Context, resolved *resolvedContext, projectID int, key string) (map[string]any, error) {
	id, found, err := lookupFlagByKey(ctx, resolved, projectID, key)
	if err != nil || !found {
		return nil, err
	}
	return fetchFlag(ctx, resolved, projectID, id)
}

func flagKeyByID(ctx context.Context, resolved *resolvedContext, projectID int, id string) (string, error) {
	raw, err := resolved.Client.Get(ctx, flagPath(projectID, id), nil)
	if err != nil {
		return "", err
	}
	var flag struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &flag); err != nil {
		return "", err
	}
	return flag.Key, nil
}

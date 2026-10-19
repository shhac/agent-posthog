package featureflag

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

// Key lookups page through matches this far before refusing to guess.
var (
	keyLookupLimit = 100
	keyLookupPages = 20
)

// ParseRef splits a flag reference into an ID or a key. "id:<n>" and "key:<k>"
// say which one is meant; a bare number is an ID and anything else a key, so a
// flag whose key is a number is reached with "key:".
func ParseRef(ref string) (id, key string, err error) {
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

// ResolveID maps a flag reference to its numeric ID.
func (p Project) ResolveID(ctx context.Context, ref string) (string, error) {
	id, key, err := ParseRef(ref)
	if err != nil || id != "" {
		return id, err
	}
	id, found, err := p.FindByKey(ctx, key)
	if err != nil {
		return "", err
	}
	if !found {
		return "", agenterrors.New("No feature flag matched key "+key, agenterrors.FixableByAgent).
			WithHint("Run 'agent-posthog flags list --search " + key + "' to inspect matches.")
	}
	return id, nil
}

// Get reads a flag or one of its subresources (dependent_flags, activity) by
// ID, as PostHog returns it.
func (p Project) Get(ctx context.Context, id string, subresource ...string) (json.RawMessage, error) {
	return p.Client.Get(ctx, p.flagPath(id, subresource...), nil)
}

// Fetch resolves and reads a flag with lossless number decoding, for callers
// that write back what they read.
func (p Project) Fetch(ctx context.Context, ref string) (map[string]any, error) {
	id, err := p.ResolveID(ctx, ref)
	if err != nil {
		return nil, err
	}
	raw, err := p.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	flag, err := flagspec.DecodeObject(raw)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("PostHog returned an unexpected feature flag shape")
	}
	return flag, nil
}

// FindByKey finds a flag by key only, never treating the argument as an ID,
// so a spec whose key happens to be numeric still matches by key. Archived
// flags are searched after active ones.
func (p Project) FindByKey(ctx context.Context, key string) (string, bool, error) {
	for _, archived := range []bool{false, true} {
		ids, err := p.idsByKey(ctx, key, archived)
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

func (p Project) idsByKey(ctx context.Context, key string, archived bool) ([]string, error) {
	q := url.Values{}
	// key filters exactly where supported; search narrows the results if not.
	q.Set("key", key)
	q.Set("search", key)
	q.Set("limit", strconv.Itoa(keyLookupLimit))
	if archived {
		q.Set("archived", "true")
	}
	results, next, err := p.Client.ListAll(ctx, p.flagsPath(), q, keyLookupPages)
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

// WithKey reads the flag with key, or nil if the project has none.
func (p Project) WithKey(ctx context.Context, key string) (map[string]any, error) {
	id, found, err := p.FindByKey(ctx, key)
	if err != nil || !found {
		return nil, err
	}
	return p.Fetch(ctx, id)
}

// KeyByID reads a flag's key.
func (p Project) KeyByID(ctx context.Context, id string) (string, error) {
	raw, err := p.Get(ctx, id)
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

// Resolver resolves flag dependency references inside --where expressions.
func (p Project) Resolver(ctx context.Context) flagspec.FlagResolver {
	return func(ref string) (string, error) {
		return p.ResolveID(ctx, ref)
	}
}

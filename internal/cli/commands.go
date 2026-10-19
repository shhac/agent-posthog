package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	libcli "github.com/shhac/lib-agent-cli/cli"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/output"
)

// listSpec describes a list command: where the collection lives, which query
// parameters its flags set, and an optional client-side filter.
type listSpec struct {
	use, short string
	scope      scope
	collection []string
	query      func(url.Values)
	filter     func([]json.RawMessage) []json.RawMessage
}

func listCommand(globals *GlobalFlags, spec listSpec) *cobra.Command {
	var opts listOptions
	cmd := &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				path, err := resolved.path(spec.scope, spec.collection...)
				if err != nil {
					return err
				}
				q := url.Values{}
				if spec.query != nil {
					spec.query(q)
				}
				items, nextURL, err := collectList(ctx, resolved, path, q, opts)
				if err != nil {
					return err
				}
				if spec.filter != nil {
					items = spec.filter(items)
				}
				return writeListResource(items, nextURL, globals.Format, globals.Full)
			})
		},
	}
	addListPagingFlags(cmd, &opts, 100)
	return cmd
}

// fetchFunc turns one reference into the record a get command prints.
type fetchFunc func(ctx context.Context, resolved *resolvedContext, ref string) (any, error)

// entityGetCommand serves the family get contract (1..N references, NDJSON by
// default, unresolved references reported per item). The context is resolved
// and its scope checked once up front, so configuration problems fail the
// command and --timeout applies to every fetch.
func entityGetCommand(use, short string, globals *GlobalFlags, s scope, fetch fetchFunc) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := resolved.require(s); err != nil {
					return err
				}
				return libcli.EntityGet(output.Stdout(), globals.Format, args, func(ref string) (any, error) {
					return fetch(ctx, resolved, ref)
				})
			})
		},
	}
}

// getByPath fetches <scope>/<collection>/<ref>/.
func getByPath(s scope, collection ...string) fetchFunc {
	return func(ctx context.Context, resolved *resolvedContext, ref string) (any, error) {
		path, err := resolved.path(s, append(slices.Clone(collection), ref)...)
		if err != nil {
			return nil, err
		}
		raw, err := resolved.Client.Get(ctx, path, nil)
		if err != nil {
			return nil, err
		}
		return decodeRaw(raw), nil
	}
}

// subresourceCommand reads one thing that hangs off an entity, such as a
// person's activity or a dashboard run.
func subresourceCommand(use, short string, globals *GlobalFlags, s scope, segments func(ref string) []string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				path, err := resolved.path(s, segments(args[0])...)
				if err != nil {
					return err
				}
				raw, err := resolved.Client.Get(ctx, path, nil)
				if err != nil {
					return err
				}
				return writeRawResource(raw, globals.Format, globals.Full)
			})
		},
	}
}

// registerDomain adds a <name> list/get command group for one PostHog
// collection, plus any extra subcommands.
func registerDomain(root *cobra.Command, globals *GlobalFlags, name, short string, s scope, collection string, extra ...*cobra.Command) {
	cmd := &cobra.Command{Use: name, Short: short}
	cmd.AddCommand(listCommand(globals, listSpec{use: "list", short: "List " + name, scope: s, collection: []string{collection}}))
	cmd.AddCommand(entityGetCommand("get <id>...", "Get one or more "+name, globals, s, getByPath(s, collection)))
	cmd.AddCommand(extra...)
	root.AddCommand(cmd)
}

// searchFilter keeps items whose JSON contains search, case-insensitively.
func searchFilter(search *string) func([]json.RawMessage) []json.RawMessage {
	return func(items []json.RawMessage) []json.RawMessage {
		if *search == "" {
			return items
		}
		needle := strings.ToLower(*search)
		out := make([]json.RawMessage, 0, len(items))
		for _, raw := range items {
			if strings.Contains(strings.ToLower(string(raw)), needle) {
				out = append(out, raw)
			}
		}
		return out
	}
}

// setIf sets a query parameter only when the flag has a value.
func setIf(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

// decodeRaw unmarshals a JSON blob for EntityGet, cleaned like every other
// read (nils pruned, secrets redacted), since EntityGet's printer does not.
// Falls back to the raw bytes on decode failure to preserve the graceful
// degradation property: never hard-fail on output that doesn't parse.
func decodeRaw(raw json.RawMessage) any {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	return output.Clean(v)
}

// resolveByField looks up a single record by the given field value in a list.
// It returns a FixableByAgent error if zero or multiple records match, keeping
// misses as item-level unresolved rather than aborting the batch.
func resolveByField(items []json.RawMessage, field, value, label string) (any, error) {
	var matches []any
	for _, raw := range items {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if fmt.Sprint(m[field]) == value {
			matches = append(matches, m)
		}
	}
	if len(matches) == 0 {
		return nil, agenterrors.New("No "+label+" matched "+value, agenterrors.FixableByAgent)
	}
	if len(matches) > 1 {
		return nil, agenterrors.New("Multiple "+label+" records matched "+value, agenterrors.FixableByAgent).
			WithHint("Use the numeric ID instead.")
	}
	return matches[0], nil
}

package shared

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	libcli "github.com/shhac/lib-agent-cli/cli"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/output"
)

// ListSpec describes a list command: where the collection lives, which query
// parameters its flags set, and an optional client-side filter.
type ListSpec struct {
	Use, Short string
	Scope      Scope
	Collection string
	Query      func(url.Values)
	Filter     func([]json.RawMessage) []json.RawMessage
}

func ListCommand(globals *GlobalFlags, spec ListSpec) *cobra.Command {
	var opts listOptions
	cmd := &cobra.Command{
		Use:   spec.Use,
		Short: spec.Short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return WithClient(cmd.Context(), globals, func(ctx context.Context, resolved *Resolved) error {
				path, err := resolved.Path(spec.Scope, spec.Collection)
				if err != nil {
					return err
				}
				q := url.Values{}
				if spec.Query != nil {
					spec.Query(q)
				}
				items, nextURL, err := collectList(ctx, resolved, path, q, opts)
				if err != nil {
					return err
				}
				if spec.Filter != nil {
					items = spec.Filter(items)
				}
				return WriteRawList(items, nextURL, globals.Format, ShapeOf(globals))
			})
		},
	}
	addListPagingFlags(cmd, &opts, 100)
	return cmd
}

// FetchFunc turns one reference into the record a get command prints.
type FetchFunc func(ctx context.Context, resolved *Resolved, ref string) (any, error)

// EntityGetCommand serves the family get contract (1..N references, NDJSON by
// default, unresolved references reported per item). The context is resolved
// and its scope checked once up front, so configuration problems fail the
// command and --timeout applies to every fetch.
func EntityGetCommand(use, short string, globals *GlobalFlags, s Scope, fetch FetchFunc) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return WithClient(cmd.Context(), globals, func(ctx context.Context, resolved *Resolved) error {
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

// GetByPath fetches <scope>/<collection>/<ref>/.
func GetByPath(s Scope, collection string) FetchFunc {
	return func(ctx context.Context, resolved *Resolved, ref string) (any, error) {
		path, err := resolved.Path(s, collection, ref)
		if err != nil {
			return nil, err
		}
		raw, err := resolved.Client.Get(ctx, path, nil)
		if err != nil {
			return nil, err
		}
		return DecodeRaw(raw), nil
	}
}

// SubresourceCommand reads one thing that hangs off an entity, such as a
// person's activity or a dashboard run.
func SubresourceCommand(use, short string, globals *GlobalFlags, s Scope, segments func(ref string) []string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return WithClient(cmd.Context(), globals, func(ctx context.Context, resolved *Resolved) error {
				path, err := resolved.Path(s, segments(args[0])...)
				if err != nil {
					return err
				}
				raw, err := resolved.Client.Get(ctx, path, nil)
				if err != nil {
					return err
				}
				return WriteRaw(raw, globals.Format, ShapeOf(globals))
			})
		},
	}
}

// Domain is a read-only command group: list, get by ID, and any extra
// commands.
type Domain struct {
	Name    string
	Aliases []string
	Short   string
	// Noun names the records in help ("List <noun>"); it defaults to Name.
	Noun string
	// IDArg is the get argument's placeholder; it defaults to <id>.
	IDArg      string
	Scope      Scope
	Collection string
	Extra      []*cobra.Command
}

// RegisterDomain adds a read-only command group to root.
func RegisterDomain(root *cobra.Command, globals *GlobalFlags, d Domain) {
	noun := cmp.Or(d.Noun, d.Name)
	cmd := &cobra.Command{Use: d.Name, Aliases: d.Aliases, Short: d.Short}
	cmd.AddCommand(ListCommand(globals, ListSpec{Use: "list", Short: "List " + noun, Scope: d.Scope, Collection: d.Collection}))
	cmd.AddCommand(EntityGetCommand("get "+cmp.Or(d.IDArg, "<id>")+"...", "Get one or more "+noun, globals, d.Scope, GetByPath(d.Scope, d.Collection)))
	cmd.AddCommand(d.Extra...)
	root.AddCommand(cmd)
}

// SearchFilter keeps items whose JSON contains search, case-insensitively.
func SearchFilter(search *string) func([]json.RawMessage) []json.RawMessage {
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

// SetIf sets a query parameter only when the flag has a value.
func SetIf(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

// DecodeRaw unmarshals a JSON blob for EntityGet, cleaned like every other
// read (nils pruned, secrets redacted), since EntityGet's printer does not.
// Falls back to the raw bytes on decode failure to preserve the graceful
// degradation property: never hard-fail on output that doesn't parse.
func DecodeRaw(raw json.RawMessage) any {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	return output.Clean(v)
}

// ResolveByField looks up a single record by the given field value in a list.
// It returns a FixableByAgent error if zero or multiple records match, keeping
// misses as item-level unresolved rather than aborting the batch.
func ResolveByField(items []json.RawMessage, field, value, label string) (any, error) {
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

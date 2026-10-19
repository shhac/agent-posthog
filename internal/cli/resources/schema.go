package resources

import (
	"context"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/output"
)

func registerSchema(root *cobra.Command, globals *shared.GlobalFlags) {
	schema := &cobra.Command{Use: "schema", Short: "Inspect event and property definitions"}
	schema.AddCommand(schemaEventsCommand(globals))
	schema.AddCommand(schemaPropertiesCommand(globals))
	root.AddCommand(schema)
}

func schemaEventsCommand(globals *shared.GlobalFlags) *cobra.Command {
	events := &cobra.Command{Use: "events", Short: "List and get event definitions"}
	var search string
	var excludeHidden, excludeStale bool
	list := shared.ListCommand(globals, shared.ListSpec{
		Use: "list", Short: "List event definitions", Scope: shared.ScopeProject, Collection: "event_definitions",
		Query: func(q url.Values) {
			if excludeHidden {
				q.Set("exclude_hidden", "true")
			}
			if excludeStale {
				q.Set("exclude_stale", "true")
			}
		},
		Filter: shared.SearchFilter(&search),
	})
	list.Flags().StringVar(&search, "search", "", "Client-side substring filter on event name")
	list.Flags().BoolVar(&excludeHidden, "exclude-hidden", false, "Exclude hidden event definitions")
	list.Flags().BoolVar(&excludeStale, "exclude-stale", false, "Exclude stale event definitions")
	events.AddCommand(list)
	events.AddCommand(shared.EntityGetCommand("get <event-id-or-name>...", "Get one or more event definitions by ID or name", globals, shared.ScopeProject, fetchEventDefinition))
	return events
}

// fetchEventDefinition reads an event definition by numeric ID, or finds it
// by exact name.
func fetchEventDefinition(ctx context.Context, resolved *shared.Resolved, ref string) (any, error) {
	if _, err := strconv.Atoi(ref); err == nil {
		return shared.GetByPath(shared.ScopeProject, "event_definitions")(ctx, resolved, ref)
	}
	path, err := resolved.Path(shared.ScopeProject, "event_definitions")
	if err != nil {
		return nil, err
	}
	page, err := resolved.Client.List(ctx, path, url.Values{"limit": {"200"}})
	if err != nil {
		return nil, err
	}
	record, err := shared.ResolveByField(page.Results, "name", ref, "event definition")
	if err != nil {
		return nil, err
	}
	return output.Clean(record), nil
}

func schemaPropertiesCommand(globals *shared.GlobalFlags) *cobra.Command {
	properties := &cobra.Command{Use: "properties", Short: "List property definitions"}
	var propType, eventName, search string
	properties.AddCommand(shared.ListCommand(globals, shared.ListSpec{
		Use: "list", Short: "List property definitions", Scope: shared.ScopeProject, Collection: "property_definitions",
		Query: func(q url.Values) {
			shared.SetIf(q, "type", propType)
			if eventName != "" {
				q.Set("event_names", eventName)
				q.Set("filter_by_event_names", "true")
			}
		},
		Filter: shared.SearchFilter(&search),
	}))
	properties.PersistentFlags().StringVar(&propType, "type", "", "Property type filter, such as event or person")
	properties.PersistentFlags().StringVar(&eventName, "event", "", "Event name filter")
	properties.PersistentFlags().StringVar(&search, "search", "", "Client-side substring filter on property name")
	return properties
}

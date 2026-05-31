package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
)

func registerOrgs(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "orgs", Short: "List and get PostHog organizations"}
	cmd.AddCommand(listCommand("list", "List organizations", globals, func(ctx *resolvedContext) (string, url.Values, error) {
		return "/api/organizations/", url.Values{}, nil
	}))
	cmd.AddCommand(getCommand("get <org-id>", "Get an organization", globals, func(ctx *resolvedContext, id string) (string, error) {
		return "/api/organizations/" + id + "/", nil
	}))
	root.AddCommand(cmd)
}

func registerProjects(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "projects", Short: "List and get PostHog projects"}
	cmd.AddCommand(listCommand("list", "List projects", globals, func(ctx *resolvedContext) (string, url.Values, error) {
		if err := requireOrg(ctx); err != nil {
			return "", nil, err
		}
		return "/api/organizations/" + ctx.OrgID + "/projects/", url.Values{}, nil
	}))
	cmd.AddCommand(getCommand("get <project-id>", "Get a project", globals, func(ctx *resolvedContext, id string) (string, error) {
		if err := requireOrg(ctx); err != nil {
			return "", err
		}
		return "/api/organizations/" + ctx.OrgID + "/projects/" + id + "/", nil
	}))
	root.AddCommand(cmd)
}

func registerEnvironments(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "environments", Aliases: []string{"envs"}, Short: "List and get PostHog environments"}
	cmd.AddCommand(listCommand("list", "List environments", globals, func(ctx *resolvedContext) (string, url.Values, error) {
		if err := requireProject(ctx); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("/api/projects/%d/environments/", ctx.ProjectID), url.Values{}, nil
	}))
	cmd.AddCommand(getCommand("get <environment-id>", "Get an environment", globals, func(ctx *resolvedContext, id string) (string, error) {
		if err := requireProject(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("/api/projects/%d/environments/%s/", ctx.ProjectID, id), nil
	}))
	root.AddCommand(cmd)
}

func registerSchema(root *cobra.Command, globals *GlobalFlags) {
	schema := &cobra.Command{Use: "schema", Short: "Inspect event and property definitions"}
	events := &cobra.Command{Use: "events", Short: "List and get event definitions"}
	var search string
	var excludeHidden, excludeStale bool
	var eventListOpts listOptions
	eventsList := &cobra.Command{
		Use:   "list",
		Short: "List event definitions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				q := baseValues(0)
				if excludeHidden {
					q.Set("exclude_hidden", "true")
				}
				if excludeStale {
					q.Set("exclude_stale", "true")
				}
				items, nextURL, err := collectList(ctx, resolved, fmt.Sprintf("/api/projects/%d/event_definitions/", resolved.ProjectID), q, eventListOpts)
				if err != nil {
					return err
				}
				return writeListResource(filterRaw(items, search), nextURL, globals.Format, globals.Full)
			})
		},
	}
	eventsList.Flags().StringVar(&search, "search", "", "Client-side substring filter on event name")
	eventsList.Flags().BoolVar(&excludeHidden, "exclude-hidden", false, "Exclude hidden event definitions")
	eventsList.Flags().BoolVar(&excludeStale, "exclude-stale", false, "Exclude stale event definitions")
	addListPagingFlags(eventsList, &eventListOpts, 100)
	events.AddCommand(eventsList)
	events.AddCommand(&cobra.Command{
		Use:   "get <event-id-or-name>",
		Short: "Get an event definition by ID or name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				if _, err := strconv.Atoi(args[0]); err == nil {
					raw, err := resolved.Client.Get(ctx, fmt.Sprintf("/api/projects/%d/event_definitions/%s/", resolved.ProjectID, args[0]), nil)
					if err != nil {
						return err
					}
					return writeRaw(raw, globals.Format)
				}
				q := baseValues(200)
				page, err := resolved.Client.List(ctx, fmt.Sprintf("/api/projects/%d/event_definitions/", resolved.ProjectID), q)
				if err != nil {
					return err
				}
				return writeResolvedByField(page.Results, "name", args[0], globals.Format, "event definition")
			})
		},
	})
	schema.AddCommand(events)

	properties := &cobra.Command{Use: "properties", Short: "List property definitions"}
	var propType, eventName, propSearch string
	var propListOpts listOptions
	properties.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List property definitions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				q := baseValues(0)
				if propType != "" {
					q.Set("type", propType)
				}
				if eventName != "" {
					q.Set("event_names", eventName)
					q.Set("filter_by_event_names", "true")
				}
				items, nextURL, err := collectList(ctx, resolved, fmt.Sprintf("/api/projects/%d/property_definitions/", resolved.ProjectID), q, propListOpts)
				if err != nil {
					return err
				}
				return writeListResource(filterRaw(items, propSearch), nextURL, globals.Format, globals.Full)
			})
		},
	})
	properties.PersistentFlags().StringVar(&propType, "type", "", "Property type filter, such as event or person")
	properties.PersistentFlags().StringVar(&eventName, "event", "", "Event name filter")
	properties.PersistentFlags().StringVar(&propSearch, "search", "", "Client-side substring filter on property name")
	addListPagingFlags(properties.Commands()[0], &propListOpts, 100)
	schema.AddCommand(properties)
	root.AddCommand(schema)
}

func registerPersons(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "persons", Short: "List and get PostHog persons"}
	var email, distinctID string
	var opts listOptions
	list := &cobra.Command{
		Use:   "list",
		Short: "List persons",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireEnvironment(resolved); err != nil {
					return err
				}
				q := baseValues(0)
				if email != "" {
					q.Set("email", email)
				}
				if distinctID != "" {
					q.Set("distinct_id", distinctID)
				}
				items, nextURL, err := collectList(ctx, resolved, fmt.Sprintf("/api/environments/%d/persons/", resolved.EnvironmentID), q, opts)
				if err != nil {
					return err
				}
				return writeListResource(items, nextURL, globals.Format, globals.Full)
			})
		},
	}
	list.Flags().StringVar(&email, "email", "", "Email filter")
	list.Flags().StringVar(&distinctID, "distinct-id", "", "Distinct ID filter")
	addListPagingFlags(list, &opts, 100)
	cmd.AddCommand(list)
	cmd.AddCommand(getCommand("get <person-id>", "Get a person", globals, func(ctx *resolvedContext, id string) (string, error) {
		if err := requireEnvironment(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("/api/environments/%d/persons/%s/", ctx.EnvironmentID, id), nil
	}))
	cmd.AddCommand(getCommand("activity <person-id>", "Get person activity", globals, func(ctx *resolvedContext, id string) (string, error) {
		if err := requireEnvironment(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("/api/environments/%d/persons/%s/activity/", ctx.EnvironmentID, id), nil
	}))
	root.AddCommand(cmd)
}

func registerFlags(root *cobra.Command, globals *GlobalFlags) {
	flags := &cobra.Command{Use: "flags", Short: "List and get PostHog feature flags"}
	var active string
	var search string
	var opts listOptions
	list := &cobra.Command{
		Use:   "list",
		Short: "List feature flags",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				q := baseValues(0)
				if active != "" {
					q.Set("active", active)
				}
				if search != "" {
					q.Set("search", search)
				}
				items, nextURL, err := collectList(ctx, resolved, fmt.Sprintf("/api/projects/%d/feature_flags/", resolved.ProjectID), q, opts)
				if err != nil {
					return err
				}
				return writeListResource(items, nextURL, globals.Format, globals.Full)
			})
		},
	}
	list.Flags().StringVar(&active, "active", "", "Active filter: true or false")
	list.Flags().StringVar(&search, "search", "", "Search feature flags")
	addListPagingFlags(list, &opts, 100)
	flags.AddCommand(list)
	flags.AddCommand(flagGetCommand(globals, "get <id-or-key>", "Get a feature flag", ""))
	flags.AddCommand(flagGetCommand(globals, "dependent <id-or-key>", "List dependent feature flags", "dependent_flags"))
	flags.AddCommand(flagGetCommand(globals, "activity <id-or-key>", "Get feature flag activity", "activity"))
	root.AddCommand(flags)
}

func flagGetCommand(globals *GlobalFlags, use, short, suffix string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				if err := requireProject(resolved); err != nil {
					return err
				}
				id, err := resolveFlagID(ctx, resolved, args[0])
				if err != nil {
					return err
				}
				path := fmt.Sprintf("/api/projects/%d/feature_flags/%s/", resolved.ProjectID, id)
				if suffix != "" {
					path += suffix + "/"
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

func registerGenericDomains(root *cobra.Command, globals *GlobalFlags) {
	registerEnvironmentDomain(root, globals, "insights", "PostHog insights", "insights")
	registerEnvironmentDomain(root, globals, "dashboards", "PostHog dashboards", "dashboards")
	registerEnvironmentDomain(root, globals, "recordings", "PostHog session recordings", "session_recordings")
	registerProjectDomain(root, globals, "experiments", "PostHog experiments", "experiments")
}

func registerEnvironmentDomain(root *cobra.Command, globals *GlobalFlags, name, short, apiName string) {
	cmd := &cobra.Command{Use: name, Short: short}
	cmd.AddCommand(listCommand("list", "List "+name, globals, func(ctx *resolvedContext) (string, url.Values, error) {
		if err := requireEnvironment(ctx); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("/api/environments/%d/%s/", ctx.EnvironmentID, apiName), url.Values{}, nil
	}))
	cmd.AddCommand(getCommand("get <id>", "Get "+name, globals, func(ctx *resolvedContext, id string) (string, error) {
		if err := requireEnvironment(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("/api/environments/%d/%s/%s/", ctx.EnvironmentID, apiName, id), nil
	}))
	if name == "dashboards" {
		cmd.AddCommand(getCommand("run <id>", "Run dashboard insights", globals, func(ctx *resolvedContext, id string) (string, error) {
			if err := requireEnvironment(ctx); err != nil {
				return "", err
			}
			return fmt.Sprintf("/api/environments/%d/dashboards/%s/run_insights/", ctx.EnvironmentID, id), nil
		}))
	}
	root.AddCommand(cmd)
}

func registerProjectDomain(root *cobra.Command, globals *GlobalFlags, name, short, apiName string) {
	cmd := &cobra.Command{Use: name, Short: short}
	cmd.AddCommand(listCommand("list", "List "+name, globals, func(ctx *resolvedContext) (string, url.Values, error) {
		if err := requireProject(ctx); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("/api/projects/%d/%s/", ctx.ProjectID, apiName), url.Values{}, nil
	}))
	cmd.AddCommand(getCommand("get <id>", "Get "+name, globals, func(ctx *resolvedContext, id string) (string, error) {
		if err := requireProject(ctx); err != nil {
			return "", err
		}
		return fmt.Sprintf("/api/projects/%d/%s/%s/", ctx.ProjectID, apiName, id), nil
	}))
	root.AddCommand(cmd)
}

func listCommand(use, short string, globals *GlobalFlags, pathFn func(*resolvedContext) (string, url.Values, error)) *cobra.Command {
	var opts listOptions
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				path, q, err := pathFn(resolved)
				if err != nil {
					return err
				}
				items, nextURL, err := collectList(ctx, resolved, path, q, opts)
				if err != nil {
					return err
				}
				return writeListResource(items, nextURL, globals.Format, globals.Full)
			})
		},
	}
	addListPagingFlags(cmd, &opts, 100)
	return cmd
}

func getCommand(use, short string, globals *GlobalFlags, pathFn func(*resolvedContext, string) (string, error)) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				path, err := pathFn(resolved, args[0])
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

func filterRaw(items []json.RawMessage, search string) []json.RawMessage {
	if search == "" {
		return items
	}
	out := make([]json.RawMessage, 0, len(items))
	search = strings.ToLower(search)
	for _, raw := range items {
		if strings.Contains(strings.ToLower(string(raw)), search) {
			out = append(out, raw)
		}
	}
	return out
}

func writeResolvedByField(items []json.RawMessage, field, value, format, label string) error {
	var matches []json.RawMessage
	for _, raw := range items {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if fmt.Sprint(m[field]) == value {
			matches = append(matches, raw)
		}
	}
	if len(matches) == 0 {
		return agenterrors.New("No "+label+" matched "+value, agenterrors.FixableByAgent)
	}
	if len(matches) > 1 {
		return agenterrors.New("Multiple "+label+" records matched "+value, agenterrors.FixableByAgent).
			WithHint("Use the numeric ID instead.")
	}
	return writeRaw(matches[0], format)
}

func resolveFlagID(ctx context.Context, resolved *resolvedContext, idOrKey string) (string, error) {
	if _, err := strconv.Atoi(idOrKey); err == nil {
		return idOrKey, nil
	}
	q := baseValues(100)
	q.Set("search", idOrKey)
	page, err := resolved.Client.List(ctx, fmt.Sprintf("/api/projects/%d/feature_flags/", resolved.ProjectID), q)
	if err != nil {
		return "", err
	}
	var matches []string
	for _, raw := range page.Results {
		var flag map[string]any
		if err := json.Unmarshal(raw, &flag); err != nil {
			continue
		}
		if fmt.Sprint(flag["key"]) == idOrKey {
			matches = append(matches, fmt.Sprint(flag["id"]))
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

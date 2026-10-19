package resources

import (
	"net/url"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
)

// Register adds the read-only resource groups: organizations, projects,
// environments, persons, schema, and the generic domains.
func Register(root *cobra.Command, globals *shared.GlobalFlags) {
	for _, d := range []shared.Domain{
		{Name: "orgs", Short: "List and get PostHog organizations", Noun: "organizations", IDArg: "<org-id>", Scope: shared.ScopeAPI, Collection: "organizations"},
		{Name: "projects", Short: "List and get PostHog projects", IDArg: "<project-id>", Scope: shared.ScopeOrg, Collection: "projects"},
		{Name: "environments", Aliases: []string{"envs"}, Short: "List and get PostHog environments", IDArg: "<environment-id>", Scope: shared.ScopeProject, Collection: "environments"},
		{Name: "insights", Short: "PostHog insights", Scope: shared.ScopeEnvironment, Collection: "insights"},
		{Name: "dashboards", Short: "PostHog dashboards", Scope: shared.ScopeEnvironment, Collection: "dashboards", Extra: []*cobra.Command{
			shared.SubresourceCommand("run <id>", "Run dashboard insights", globals, shared.ScopeEnvironment, func(id string) []string {
				return []string{"dashboards", id, "run_insights"}
			}),
		}},
		{Name: "recordings", Short: "PostHog session recordings", Scope: shared.ScopeEnvironment, Collection: "session_recordings"},
		{Name: "experiments", Short: "PostHog experiments", Scope: shared.ScopeProject, Collection: "experiments"},
	} {
		shared.RegisterDomain(root, globals, d)
	}
	registerPersons(root, globals)
	registerSchema(root, globals)
}

func registerPersons(root *cobra.Command, globals *shared.GlobalFlags) {
	cmd := &cobra.Command{Use: "persons", Short: "List and get PostHog persons"}
	var email, distinctID string
	list := shared.ListCommand(globals, shared.ListSpec{
		Use: "list", Short: "List persons", Scope: shared.ScopeEnvironment, Collection: "persons",
		Query: func(q url.Values) {
			shared.SetIf(q, "email", email)
			shared.SetIf(q, "distinct_id", distinctID)
		},
	})
	list.Flags().StringVar(&email, "email", "", "Email filter")
	list.Flags().StringVar(&distinctID, "distinct-id", "", "Distinct ID filter")
	cmd.AddCommand(list)
	cmd.AddCommand(shared.EntityGetCommand("get <person-id>...", "Get one or more persons", globals, shared.ScopeEnvironment, shared.GetByPath(shared.ScopeEnvironment, "persons")))
	cmd.AddCommand(shared.SubresourceCommand("activity <person-id>", "Get person activity", globals, shared.ScopeEnvironment, func(id string) []string {
		return []string{"persons", id, "activity"}
	}))
	root.AddCommand(cmd)
}

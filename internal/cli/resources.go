package cli

import (
	"net/url"

	"github.com/spf13/cobra"
)

func registerOrgs(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "orgs", Short: "List and get PostHog organizations"}
	cmd.AddCommand(ListCommand(globals, ListSpec{Use: "list", Short: "List organizations", Scope: ScopeAPI, Collection: []string{"organizations"}}))
	cmd.AddCommand(EntityGetCommand("get <org-id>...", "Get one or more organizations", globals, ScopeAPI, GetByPath(ScopeAPI, "organizations")))
	root.AddCommand(cmd)
}

func registerProjects(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "projects", Short: "List and get PostHog projects"}
	cmd.AddCommand(ListCommand(globals, ListSpec{Use: "list", Short: "List projects", Scope: ScopeOrg, Collection: []string{"projects"}}))
	cmd.AddCommand(EntityGetCommand("get <project-id>...", "Get one or more projects", globals, ScopeOrg, GetByPath(ScopeOrg, "projects")))
	root.AddCommand(cmd)
}

func registerEnvironments(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "environments", Aliases: []string{"envs"}, Short: "List and get PostHog environments"}
	cmd.AddCommand(ListCommand(globals, ListSpec{Use: "list", Short: "List environments", Scope: ScopeProject, Collection: []string{"environments"}}))
	cmd.AddCommand(EntityGetCommand("get <environment-id>...", "Get one or more environments", globals, ScopeProject, GetByPath(ScopeProject, "environments")))
	root.AddCommand(cmd)
}

func registerPersons(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "persons", Short: "List and get PostHog persons"}
	var email, distinctID string
	list := ListCommand(globals, ListSpec{
		Use: "list", Short: "List persons", Scope: ScopeEnvironment, Collection: []string{"persons"},
		Query: func(q url.Values) {
			SetIf(q, "email", email)
			SetIf(q, "distinct_id", distinctID)
		},
	})
	list.Flags().StringVar(&email, "email", "", "Email filter")
	list.Flags().StringVar(&distinctID, "distinct-id", "", "Distinct ID filter")
	cmd.AddCommand(list)
	cmd.AddCommand(EntityGetCommand("get <person-id>...", "Get one or more persons", globals, ScopeEnvironment, GetByPath(ScopeEnvironment, "persons")))
	cmd.AddCommand(SubresourceCommand("activity <person-id>", "Get person activity", globals, ScopeEnvironment, func(id string) []string {
		return []string{"persons", id, "activity"}
	}))
	root.AddCommand(cmd)
}

func registerGenericDomains(root *cobra.Command, globals *GlobalFlags) {
	RegisterDomain(root, globals, "insights", "PostHog insights", ScopeEnvironment, "insights")
	RegisterDomain(root, globals, "dashboards", "PostHog dashboards", ScopeEnvironment, "dashboards",
		SubresourceCommand("run <id>", "Run dashboard insights", globals, ScopeEnvironment, func(id string) []string {
			return []string{"dashboards", id, "run_insights"}
		}))
	RegisterDomain(root, globals, "recordings", "PostHog session recordings", ScopeEnvironment, "session_recordings")
	RegisterDomain(root, globals, "experiments", "PostHog experiments", ScopeProject, "experiments")
}

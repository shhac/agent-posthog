package resources

import (
	"net/url"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
)

// Register adds the read-only resource groups: organizations, projects,
// environments, persons, schema, and the generic domains.
func Register(root *cobra.Command, globals *shared.GlobalFlags) {
	registerOrgs(root, globals)
	registerProjects(root, globals)
	registerEnvironments(root, globals)
	registerPersons(root, globals)
	registerSchema(root, globals)
	registerGenericDomains(root, globals)
}

func registerOrgs(root *cobra.Command, globals *shared.GlobalFlags) {
	cmd := &cobra.Command{Use: "orgs", Short: "List and get PostHog organizations"}
	cmd.AddCommand(shared.ListCommand(globals, shared.ListSpec{Use: "list", Short: "List organizations", Scope: shared.ScopeAPI, Collection: []string{"organizations"}}))
	cmd.AddCommand(shared.EntityGetCommand("get <org-id>...", "Get one or more organizations", globals, shared.ScopeAPI, shared.GetByPath(shared.ScopeAPI, "organizations")))
	root.AddCommand(cmd)
}

func registerProjects(root *cobra.Command, globals *shared.GlobalFlags) {
	cmd := &cobra.Command{Use: "projects", Short: "List and get PostHog projects"}
	cmd.AddCommand(shared.ListCommand(globals, shared.ListSpec{Use: "list", Short: "List projects", Scope: shared.ScopeOrg, Collection: []string{"projects"}}))
	cmd.AddCommand(shared.EntityGetCommand("get <project-id>...", "Get one or more projects", globals, shared.ScopeOrg, shared.GetByPath(shared.ScopeOrg, "projects")))
	root.AddCommand(cmd)
}

func registerEnvironments(root *cobra.Command, globals *shared.GlobalFlags) {
	cmd := &cobra.Command{Use: "environments", Aliases: []string{"envs"}, Short: "List and get PostHog environments"}
	cmd.AddCommand(shared.ListCommand(globals, shared.ListSpec{Use: "list", Short: "List environments", Scope: shared.ScopeProject, Collection: []string{"environments"}}))
	cmd.AddCommand(shared.EntityGetCommand("get <environment-id>...", "Get one or more environments", globals, shared.ScopeProject, shared.GetByPath(shared.ScopeProject, "environments")))
	root.AddCommand(cmd)
}

func registerPersons(root *cobra.Command, globals *shared.GlobalFlags) {
	cmd := &cobra.Command{Use: "persons", Short: "List and get PostHog persons"}
	var email, distinctID string
	list := shared.ListCommand(globals, shared.ListSpec{
		Use: "list", Short: "List persons", Scope: shared.ScopeEnvironment, Collection: []string{"persons"},
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

func registerGenericDomains(root *cobra.Command, globals *shared.GlobalFlags) {
	shared.RegisterDomain(root, globals, "insights", "PostHog insights", shared.ScopeEnvironment, "insights")
	shared.RegisterDomain(root, globals, "dashboards", "PostHog dashboards", shared.ScopeEnvironment, "dashboards",
		shared.SubresourceCommand("run <id>", "Run dashboard insights", globals, shared.ScopeEnvironment, func(id string) []string {
			return []string{"dashboards", id, "run_insights"}
		}))
	shared.RegisterDomain(root, globals, "recordings", "PostHog session recordings", shared.ScopeEnvironment, "session_recordings")
	shared.RegisterDomain(root, globals, "experiments", "PostHog experiments", shared.ScopeProject, "experiments")
}

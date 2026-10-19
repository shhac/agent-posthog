package cli

import (
	"net/url"

	"github.com/spf13/cobra"
)

func registerOrgs(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "orgs", Short: "List and get PostHog organizations"}
	cmd.AddCommand(listCommand(globals, listSpec{use: "list", short: "List organizations", scope: scopeAPI, collection: []string{"organizations"}}))
	cmd.AddCommand(entityGetCommand("get <org-id>...", "Get one or more organizations", globals, scopeAPI, getByPath(scopeAPI, "organizations")))
	root.AddCommand(cmd)
}

func registerProjects(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "projects", Short: "List and get PostHog projects"}
	cmd.AddCommand(listCommand(globals, listSpec{use: "list", short: "List projects", scope: scopeOrg, collection: []string{"projects"}}))
	cmd.AddCommand(entityGetCommand("get <project-id>...", "Get one or more projects", globals, scopeOrg, getByPath(scopeOrg, "projects")))
	root.AddCommand(cmd)
}

func registerEnvironments(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "environments", Aliases: []string{"envs"}, Short: "List and get PostHog environments"}
	cmd.AddCommand(listCommand(globals, listSpec{use: "list", short: "List environments", scope: scopeProject, collection: []string{"environments"}}))
	cmd.AddCommand(entityGetCommand("get <environment-id>...", "Get one or more environments", globals, scopeProject, getByPath(scopeProject, "environments")))
	root.AddCommand(cmd)
}

func registerPersons(root *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{Use: "persons", Short: "List and get PostHog persons"}
	var email, distinctID string
	list := listCommand(globals, listSpec{
		use: "list", short: "List persons", scope: scopeEnvironment, collection: []string{"persons"},
		query: func(q url.Values) {
			setIf(q, "email", email)
			setIf(q, "distinct_id", distinctID)
		},
	})
	list.Flags().StringVar(&email, "email", "", "Email filter")
	list.Flags().StringVar(&distinctID, "distinct-id", "", "Distinct ID filter")
	cmd.AddCommand(list)
	cmd.AddCommand(entityGetCommand("get <person-id>...", "Get one or more persons", globals, scopeEnvironment, getByPath(scopeEnvironment, "persons")))
	cmd.AddCommand(subresourceCommand("activity <person-id>", "Get person activity", globals, scopeEnvironment, func(id string) []string {
		return []string{"persons", id, "activity"}
	}))
	root.AddCommand(cmd)
}

func registerGenericDomains(root *cobra.Command, globals *GlobalFlags) {
	registerDomain(root, globals, "insights", "PostHog insights", scopeEnvironment, "insights")
	registerDomain(root, globals, "dashboards", "PostHog dashboards", scopeEnvironment, "dashboards",
		subresourceCommand("run <id>", "Run dashboard insights", globals, scopeEnvironment, func(id string) []string {
			return []string{"dashboards", id, "run_insights"}
		}))
	registerDomain(root, globals, "recordings", "PostHog session recordings", scopeEnvironment, "session_recordings")
	registerDomain(root, globals, "experiments", "PostHog experiments", scopeProject, "experiments")
}

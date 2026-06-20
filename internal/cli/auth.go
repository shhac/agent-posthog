package cli

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/config"
	"github.com/shhac/agent-posthog/internal/credential"
	"github.com/shhac/agent-posthog/internal/dialog"
	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/output"
)

var promptSecret = dialog.PromptSecret

func registerAuth(root *cobra.Command, globals *GlobalFlags) {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Manage PostHog credentials and profiles",
	}
	registerAuthAdd(auth)
	registerAuthUpdate(auth)
	registerAuthCheck(auth, globals)
	registerAuthDefault(auth)
	registerAuthList(auth)
	registerAuthRemove(auth)
	root.AddCommand(auth)
}

func registerAuthAdd(parent *cobra.Command) {
	var apiKey, host, orgID string
	var projectID, envID int
	var form bool

	cmd := &cobra.Command{
		Use:   "add <profile>",
		Short: "Add a PostHog profile with a Keychain-stored personal API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			if form {
				filled, err := promptSecret(cmd.Context(), "agent-posthog: "+alias, "PostHog personal API key", apiKey)
				if err != nil {
					fixableBy, hint := dialog.Classify(err)
					output.WriteError(output.Stderr(), agenterrors.Wrap(err, fixableBy).WithHint(hint))
					return nil
				}
				apiKey = filled
			}
			if apiKey == "" {
				output.WriteError(output.Stderr(), agenterrors.New("missing --api-key", agenterrors.FixableByAgent).
					WithHint("Agents should use 'agent-posthog auth add <profile> --form' so the key never appears in chat."))
				return nil
			}
			storage, err := credential.Store(alias, apiKey)
			if err != nil {
				output.WriteError(output.Stderr(), agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("The personal API key was not written to disk. Fix Keychain access and retry with --form."))
				return nil
			}
			profile := config.Profile{
				Host:           host,
				OrganizationID: orgID,
				ProjectID:      projectID,
				EnvironmentID:  envID,
			}
			if err := config.StoreProfile(alias, profile); err != nil {
				output.WriteError(output.Stderr(), agenterrors.Wrap(err, agenterrors.FixableByHuman))
				return nil
			}
			cfg := config.Read()
			stored := cfg.Profiles[alias]
			return writeItem(map[string]any{
				"status":          "added",
				"profile":         alias,
				"default":         cfg.DefaultProfile == alias,
				"storage":         storage,
				"host":            stored.Host,
				"organization_id": stored.OrganizationID,
				"project_id":      stored.ProjectID,
				"environment_id":  stored.EnvironmentID,
			}, "")
		},
	}
	cmd.Flags().StringVar(&apiKey, "api-key", "", "PostHog personal API key (required unless --form is used)")
	cmd.Flags().BoolVar(&form, "form", false, "Prompt for the API key via a native OS dialog")
	cmd.Flags().StringVar(&host, "host", config.DefaultHost, "PostHog private API host")
	cmd.Flags().StringVar(&orgID, "org", "", "Default organization ID")
	cmd.Flags().IntVar(&projectID, "project", 0, "Default project ID")
	cmd.Flags().IntVar(&envID, "env", 0, "Default environment ID")
	parent.AddCommand(cmd)
}

func registerAuthUpdate(parent *cobra.Command) {
	var host, orgID string
	var projectID, envID int
	var clearOrg, clearProject, clearEnv, setDefault bool

	cmd := &cobra.Command{
		Use:   "update <profile>",
		Short: "Update non-secret profile metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			if !cmd.Flags().Changed("host") && !cmd.Flags().Changed("org") && !cmd.Flags().Changed("project") && !cmd.Flags().Changed("env") && !clearOrg && !clearProject && !clearEnv && !setDefault {
				output.WriteError(output.Stderr(), agenterrors.New("no profile updates requested", agenterrors.FixableByAgent).
					WithHint("Use --host, --org, --project, --env, --clear-org, --clear-project, --clear-env, or --default."))
				return nil
			}
			if err := config.UpdateProfile(alias, func(profile config.Profile) config.Profile {
				if cmd.Flags().Changed("host") {
					profile.Host = host
				}
				if cmd.Flags().Changed("org") {
					profile.OrganizationID = orgID
				}
				if cmd.Flags().Changed("project") {
					profile.ProjectID = projectID
				}
				if cmd.Flags().Changed("env") {
					profile.EnvironmentID = envID
				}
				if clearOrg {
					profile.OrganizationID = ""
				}
				if clearProject {
					profile.ProjectID = 0
				}
				if clearEnv {
					profile.EnvironmentID = 0
				}
				return profile
			}); err != nil {
				output.WriteError(output.Stderr(), agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("Run 'agent-posthog auth list' to see configured profiles."))
				return nil
			}
			if setDefault {
				if err := config.SetDefault(alias); err != nil {
					output.WriteError(output.Stderr(), agenterrors.Wrap(err, agenterrors.FixableByHuman))
					return nil
				}
			}
			cfg := config.Read()
			profile := cfg.Profiles[alias]
			return writeItem(map[string]any{
				"status":          "updated",
				"profile":         alias,
				"default":         cfg.DefaultProfile == alias,
				"host":            profile.Host,
				"organization_id": profile.OrganizationID,
				"project_id":      profile.ProjectID,
				"environment_id":  profile.EnvironmentID,
			}, "")
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "Default PostHog private API host")
	cmd.Flags().StringVar(&orgID, "org", "", "Default organization ID")
	cmd.Flags().IntVar(&projectID, "project", 0, "Default project ID")
	cmd.Flags().IntVar(&envID, "env", 0, "Default environment ID")
	cmd.Flags().BoolVar(&clearOrg, "clear-org", false, "Clear the default organization ID")
	cmd.Flags().BoolVar(&clearProject, "clear-project", false, "Clear the default project ID")
	cmd.Flags().BoolVar(&clearEnv, "clear-env", false, "Clear the default environment ID")
	cmd.Flags().BoolVar(&setDefault, "default", false, "Make this profile the default")
	parent.AddCommand(cmd)
}

func registerAuthCheck(parent *cobra.Command, globals *GlobalFlags) {
	cmd := &cobra.Command{
		Use:   "check [profile]",
		Short: "Verify stored credentials against PostHog",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := *globals
			if len(args) > 0 {
				flags.Profile = args[0]
			}
			return withClient(cmd.Context(), &flags, func(ctx context.Context, resolved *resolvedContext) error {
				raw, err := resolved.Client.Get(ctx, "/api/users/@me/", nil)
				if err != nil {
					return err
				}
				var me map[string]any
				_ = json.Unmarshal(raw, &me)
				return writeItem(map[string]any{
					"status":          "ok",
					"profile":         resolved.Profile,
					"host":            resolved.Host,
					"organization_id": resolved.OrgID,
					"project_id":      resolved.ProjectID,
					"environment_id":  resolved.EnvironmentID,
					"user":            me,
				}, flags.Format)
			})
		},
	}
	parent.AddCommand(cmd)
}

func registerAuthDefault(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "default <profile>",
		Short: "Set the default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.SetDefault(args[0]); err != nil {
				output.WriteError(output.Stderr(), agenterrors.Wrap(err, agenterrors.FixableByHuman))
				return nil
			}
			return writeItem(map[string]any{"status": "default_set", "profile": args[0]}, "")
		},
	}
	parent.AddCommand(cmd)
}

func registerAuthList(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured profiles without exposing secrets",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Read()
			rows := make([]json.RawMessage, 0, len(cfg.Profiles))
			for alias, profile := range cfg.Profiles {
				row, _ := json.Marshal(map[string]any{
					"profile":         alias,
					"default":         alias == cfg.DefaultProfile,
					"credential":      "keychain",
					"host":            profile.Host,
					"organization_id": profile.OrganizationID,
					"project_id":      profile.ProjectID,
					"environment_id":  profile.EnvironmentID,
				})
				rows = append(rows, row)
			}
			return writeList(rows, "", "")
		},
	}
	parent.AddCommand(cmd)
}

func registerAuthRemove(parent *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "remove <profile>",
		Short: "Remove a profile and its Keychain credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			if err := credential.Remove(alias); err != nil {
				output.WriteError(output.Stderr(), agenterrors.Wrap(err, agenterrors.FixableByHuman))
				return nil
			}
			if err := config.RemoveProfile(alias); err != nil {
				output.WriteError(output.Stderr(), agenterrors.Wrap(err, agenterrors.FixableByHuman))
				return nil
			}
			return writeItem(map[string]any{"status": "removed", "profile": alias}, "")
		},
	}
	parent.AddCommand(cmd)
}

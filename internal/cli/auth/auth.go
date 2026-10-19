package auth

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"

	clidialog "github.com/shhac/lib-agent-cli/dialog"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/cli/shared"
	"github.com/shhac/agent-posthog/internal/config"
	"github.com/shhac/agent-posthog/internal/credential"
)

// PromptSecret is a variable so tests can stand in for the native dialog.
var PromptSecret = promptSecretDialog

func Register(root *cobra.Command, globals *shared.GlobalFlags) {
	auth := &cobra.Command{
		Use:   "auth",
		Short: "Manage PostHog credentials and profiles",
	}
	registerAuthAdd(auth, globals)
	registerAuthUpdate(auth, globals)
	registerAuthCheck(auth, globals)
	registerAuthDefault(auth, globals)
	registerAuthList(auth, globals)
	registerAuthRemove(auth, globals)
	root.AddCommand(auth)
}

func registerAuthAdd(parent *cobra.Command, globals *shared.GlobalFlags) {
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
				filled, err := PromptSecret(cmd.Context(), "agent-posthog: "+alias, "PostHog personal API key", apiKey)
				if err != nil {
					fixableBy, hint := classifyDialogError(err)
					return agenterrors.Wrap(err, fixableBy).WithHint(hint)
				}
				apiKey = filled
			}
			if apiKey == "" {
				return agenterrors.New("missing --api-key", agenterrors.FixableByAgent).
					WithHint("Agents should use 'agent-posthog auth add <profile> --form' so the key never appears in chat.")
			}
			storage, err := credential.Store(alias, apiKey)
			if err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("The personal API key was not written to disk. Fix Keychain access and retry with --form.")
			}
			profile := config.Profile{
				Host:           host,
				OrganizationID: orgID,
				ProjectID:      projectID,
				EnvironmentID:  envID,
			}
			if err := config.StoreProfile(alias, profile); err != nil {
				// Without the profile nothing lists or removes the secret, so take it back.
				_ = credential.Remove(alias)
				return agenterrors.Wrap(err, agenterrors.FixableByHuman)
			}
			return shared.WriteItem(profileRecord(config.Read(), alias, map[string]any{"status": "added", "storage": storage}), globals.Format)
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

func registerAuthUpdate(parent *cobra.Command, globals *shared.GlobalFlags) {
	var host, orgID string
	var projectID, envID int
	var clearOrg, clearProject, clearEnv, setDefault bool

	cmd := &cobra.Command{
		Use:   "update <profile>",
		Short: "Update non-secret profile metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			if !clearOrg && !clearProject && !clearEnv && !setDefault && !slices.ContainsFunc([]string{"host", "org", "project", "env"}, cmd.Flags().Changed) {
				return agenterrors.New("no profile updates requested", agenterrors.FixableByAgent).
					WithHint("Use --host, --org, --project, --env, --clear-org, --clear-project, --clear-env, or --default.")
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
				return agenterrors.Wrap(err, agenterrors.FixableByHuman).
					WithHint("Run 'agent-posthog auth list' to see configured profiles.")
			}
			if setDefault {
				if err := config.SetDefault(alias); err != nil {
					return agenterrors.Wrap(err, agenterrors.FixableByHuman)
				}
			}
			return shared.WriteItem(profileRecord(config.Read(), alias, map[string]any{"status": "updated"}), globals.Format)
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

func registerAuthCheck(parent *cobra.Command, globals *shared.GlobalFlags) {
	cmd := &cobra.Command{
		Use:   "check [profile]",
		Short: "Verify stored credentials against PostHog",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := *globals
			if len(args) > 0 {
				flags.Profile = args[0]
			}
			return shared.WithClient(cmd.Context(), &flags, func(ctx context.Context, resolved *shared.Resolved) error {
				raw, err := resolved.Client.Get(ctx, "/api/users/@me/", nil)
				if err != nil {
					return err
				}
				var me map[string]any
				_ = json.Unmarshal(raw, &me)
				return shared.WriteItem(map[string]any{
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

func registerAuthDefault(parent *cobra.Command, globals *shared.GlobalFlags) {
	cmd := &cobra.Command{
		Use:   "default <profile>",
		Short: "Set the default profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.SetDefault(args[0]); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByHuman)
			}
			return shared.WriteItem(map[string]any{"status": "default_set", "profile": args[0]}, globals.Format)
		},
	}
	parent.AddCommand(cmd)
}

func registerAuthList(parent *cobra.Command, globals *shared.GlobalFlags) {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured profiles without exposing secrets",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Read()
			rows := make([]json.RawMessage, 0, len(cfg.Profiles))
			for _, alias := range slices.Sorted(maps.Keys(cfg.Profiles)) {
				row, _ := json.Marshal(profileRecord(cfg, alias, map[string]any{"credential": credential.Storage(alias)}))
				rows = append(rows, row)
			}
			return shared.WriteListResource(rows, "", globals.Format, true)
		},
	}
	parent.AddCommand(cmd)
}

func registerAuthRemove(parent *cobra.Command, globals *shared.GlobalFlags) {
	cmd := &cobra.Command{
		Use:   "remove <profile>",
		Short: "Remove a profile and its Keychain credential",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			_, hasProfile := config.Read().Profiles[alias]
			var notFound *credential.NotFoundError
			err := credential.Remove(alias)
			keyAlreadyGone := errors.As(err, &notFound) && hasProfile
			if err != nil && !keyAlreadyGone {
				return agenterrors.Wrap(err, agenterrors.FixableByHuman)
			}
			if err := config.RemoveProfile(alias); err != nil {
				return agenterrors.Wrap(err, agenterrors.FixableByHuman)
			}
			return shared.WriteItem(map[string]any{"status": "removed", "profile": alias}, globals.Format)
		},
	}
	parent.AddCommand(cmd)
}

// profileRecord reports a profile's non-secret settings, plus extra fields.
func profileRecord(cfg *config.Config, alias string, extra map[string]any) map[string]any {
	profile := cfg.Profiles[alias]
	record := map[string]any{
		"profile":         alias,
		"default":         cfg.DefaultProfile == alias,
		"host":            profile.Host,
		"organization_id": profile.OrganizationID,
		"project_id":      profile.ProjectID,
		"environment_id":  profile.EnvironmentID,
	}
	maps.Copy(record, extra)
	return record
}

// promptSecretDialog opens a masked native prompt seeded with initial, so a
// token never transits argv.
func promptSecretDialog(ctx context.Context, title, label, initial string) (string, error) {
	res, err := clidialog.Prompt(ctx, clidialog.Spec{
		Title: title,
		Items: []clidialog.Field{{
			ID:        "secret",
			Label:     label,
			InputType: clidialog.Password,
			Initial:   initial,
		}},
	})
	if err != nil {
		return "", err
	}
	for _, r := range res {
		if r.ID == "secret" {
			return r.Value, nil
		}
	}
	return "", nil
}

// classifyDialogError maps a dialog failure onto fixable_by, so a cancelled
// prompt reads as retry and a headless host as human.
func classifyDialogError(err error) (agenterrors.FixableBy, string) {
	category, hint := clidialog.ClassifyError(err)
	switch category {
	case clidialog.CategoryHuman:
		return agenterrors.FixableByHuman, hint
	case clidialog.CategoryRetry:
		return agenterrors.FixableByRetry, hint
	default:
		return agenterrors.FixableByAgent, hint
	}
}

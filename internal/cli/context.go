package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/shhac/lib-agent-cli/creds"
	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/api"
	"github.com/shhac/agent-posthog/internal/config"
	"github.com/shhac/agent-posthog/internal/credential"
	"github.com/shhac/agent-posthog/internal/output"
)

type resolvedContext struct {
	Client        *api.Client
	Profile       string
	Host          string
	OrgID         string
	ProjectID     int
	EnvironmentID int
	// ProjectAliases are the resolved profile's named projects.
	ProjectAliases map[string]int
	// ProjectAlias is the alias --project named, if it named one.
	ProjectAlias string
}

func withClient(cmdCtx context.Context, flags *GlobalFlags, fn func(context.Context, *resolvedContext) error) error {
	resolved, err := resolve(flags)
	if err != nil {
		return err
	}
	timeout := time.Duration(flags.TimeoutMS) * time.Millisecond
	ctx := cmdCtx
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return fn(ctx, resolved)
}

func resolve(flags *GlobalFlags) (*resolvedContext, error) {
	cfg := config.Read()
	profileName := selectedProfile(flags, cfg)
	profile := cfg.Profiles[profileName]

	// Resolution order: explicit flag, AGENT_POSTHOG_* env var, profile metadata,
	// PostHog-native env var, built-in default. The namespaced AGENT_POSTHOG_* var
	// wins over the profile (as AGENT_POSTHOG_BASE_URL does for host) so an agent
	// can repoint a configured CLI without editing the profile; the PostHog-native
	// var stays the lowest tier as a quick-local-use default.
	host := creds.FirstNonEmpty(flags.Host, os.Getenv("AGENT_POSTHOG_BASE_URL"), profile.Host, os.Getenv("AGENT_POSTHOG_HOST"), config.DefaultHost)
	orgID := creds.FirstNonEmpty(flags.OrgID, os.Getenv("AGENT_POSTHOG_ORGANIZATION_ID"), profile.OrganizationID, os.Getenv("POSTHOG_ORGANIZATION_ID"))
	flagProject, err := projectFromRef(flags.Project, profileName, profile.ProjectAliases)
	if err != nil {
		return nil, err
	}
	projectID := creds.FirstNonZero(flagProject, envInt("AGENT_POSTHOG_PROJECT_ID"), profile.ProjectID, envInt("POSTHOG_PROJECT_ID"))
	environmentID := creds.FirstNonZero(flags.EnvironmentID, envInt("AGENT_POSTHOG_ENVIRONMENT_ID"), profile.EnvironmentID, envInt("POSTHOG_ENVIRONMENT_ID"))
	// A project's default environment is assigned environment_id == project_id, so
	// an unset environment falls back to the project rather than erroring. Projects
	// with multiple first-class environments still target a non-default one by
	// setting it explicitly (flag, profile, or env var above).
	if environmentID == 0 {
		environmentID = projectID
	}
	token := creds.FirstNonEmpty(flags.APIKey, os.Getenv("POSTHOG_PERSONAL_API_KEY"), os.Getenv("AGENT_POSTHOG_PERSONAL_API_KEY"))
	if token == "" && profileName != "" {
		token, err = credential.Get(profileName)
		if err != nil {
			return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman).
				WithHint("Run 'agent-posthog auth add <profile> --form' to store a personal API key.")
		}
	}
	if token == "" {
		return nil, agenterrors.New("missing PostHog personal API key", agenterrors.FixableByHuman).
			WithHint("Run 'agent-posthog auth add <profile> --form' or set POSTHOG_PERSONAL_API_KEY for direct auth.")
	}
	client := api.New(host, token)
	client.Guard = requireWriteAllowed
	client.MaxRetries = flags.MaxRetries
	client.Debug = flags.Debug
	client.DebugOut = output.Stderr()
	return &resolvedContext{
		Client:         client,
		Profile:        profileName,
		Host:           host,
		OrgID:          orgID,
		ProjectID:      projectID,
		EnvironmentID:  environmentID,
		ProjectAliases: profile.ProjectAliases,
		ProjectAlias:   aliasName(flags.Project),
	}, nil
}

// projectFromRef turns a --project value into an ID: a number is an ID, any
// other value must be an alias in the resolved profile.
func projectFromRef(ref, profileName string, aliases map[string]int) (int, error) {
	if ref == "" {
		return 0, nil
	}
	if id, err := strconv.Atoi(ref); err == nil {
		if id <= 0 {
			return 0, agenterrors.New("project ID must be positive", agenterrors.FixableByAgent)
		}
		return id, nil
	}
	if id, ok := aliases[ref]; ok {
		return id, nil
	}
	where := "profile " + profileName
	if profileName == "" {
		where = "the active configuration (no profile selected)"
	}
	return 0, agenterrors.New(fmt.Sprintf("unknown project alias %q in %s", ref, where), agenterrors.FixableByAgent).
		WithHint("Run 'agent-posthog config alias list', or add one with 'agent-posthog config alias set " + ref + " <project-id>'.")
}

func aliasName(ref string) string {
	if _, err := strconv.Atoi(ref); err == nil {
		return ""
	}
	return ref
}

// projectRef resolves a project argument (ID or alias) against the resolved profile.
func (r *resolvedContext) projectRef(ref string) (int, error) {
	if ref == "" {
		return 0, agenterrors.New("missing project", agenterrors.FixableByAgent)
	}
	return projectFromRef(ref, r.Profile, r.ProjectAliases)
}

// selectedProfile is the profile a command acts as: --profile, then
// AGENT_POSTHOG_PROFILE, then the configured default.
func selectedProfile(flags *GlobalFlags, cfg *config.Config) string {
	return creds.FirstNonEmpty(flags.Profile, os.Getenv("AGENT_POSTHOG_PROFILE"), cfg.DefaultProfile)
}

func requireOrg(ctx *resolvedContext) error {
	if ctx.OrgID == "" {
		return agenterrors.New("missing organization id", agenterrors.FixableByAgent).
			WithHint("Run 'agent-posthog orgs list' or set one with 'agent-posthog auth update <profile> --org <id>'.")
	}
	return nil
}

func requireProject(ctx *resolvedContext) error {
	if ctx.ProjectID == 0 {
		return agenterrors.New("missing project id", agenterrors.FixableByAgent).
			WithHint("Run 'agent-posthog projects list --org <id>' or set one with 'agent-posthog auth update <profile> --project <id>'.")
	}
	return nil
}

func requireEnvironment(ctx *resolvedContext) error {
	if ctx.EnvironmentID == 0 {
		return agenterrors.New("missing environment id", agenterrors.FixableByAgent).
			WithHint("Run 'agent-posthog environments list --project <id>' or set one with 'agent-posthog auth update <profile> --env <id>'.")
	}
	return nil
}

func envInt(name string) int {
	value, _ := strconv.Atoi(os.Getenv(name))
	return value
}

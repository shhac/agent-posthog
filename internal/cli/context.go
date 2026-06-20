package cli

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/shhac/lib-agent-cli/creds"

	"github.com/shhac/agent-posthog/internal/api"
	"github.com/shhac/agent-posthog/internal/config"
	"github.com/shhac/agent-posthog/internal/credential"
	agenterrors "github.com/shhac/agent-posthog/internal/errors"
	"github.com/shhac/agent-posthog/internal/output"
)

type resolvedContext struct {
	Client        *api.Client
	Profile       string
	Host          string
	OrgID         string
	ProjectID     int
	EnvironmentID int
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
	profileName := creds.FirstNonEmpty(flags.Profile, os.Getenv("AGENT_POSTHOG_PROFILE"), cfg.DefaultProfile)
	profile := config.Profile{}
	if profileName != "" {
		if found, ok := cfg.Profiles[profileName]; ok {
			profile = found
		}
	}

	host := creds.FirstNonEmpty(flags.Host, os.Getenv("AGENT_POSTHOG_BASE_URL"), profile.Host, os.Getenv("AGENT_POSTHOG_HOST"), config.DefaultHost)
	orgID := creds.FirstNonEmpty(flags.OrgID, profile.OrganizationID, os.Getenv("AGENT_POSTHOG_ORGANIZATION_ID"), os.Getenv("POSTHOG_ORGANIZATION_ID"))
	projectID := creds.FirstNonZero(flags.ProjectID, profile.ProjectID, envInt("AGENT_POSTHOG_PROJECT_ID"), envInt("POSTHOG_PROJECT_ID"))
	environmentID := creds.FirstNonZero(flags.EnvironmentID, profile.EnvironmentID, envInt("AGENT_POSTHOG_ENVIRONMENT_ID"), envInt("POSTHOG_ENVIRONMENT_ID"))
	token := creds.FirstNonEmpty(flags.APIKey, os.Getenv("POSTHOG_PERSONAL_API_KEY"), os.Getenv("AGENT_POSTHOG_PERSONAL_API_KEY"))
	if token == "" && profileName != "" {
		var err error
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
	client.MaxRetries = flags.MaxRetries
	client.Debug = flags.Debug
	client.DebugOut = output.Stderr()
	return &resolvedContext{
		Client:        client,
		Profile:       profileName,
		Host:          host,
		OrgID:         orgID,
		ProjectID:     projectID,
		EnvironmentID: environmentID,
	}, nil
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

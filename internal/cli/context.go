package cli

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"time"

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
		output.WriteError(output.Stderr(), err)
		return nil
	}
	timeout := time.Duration(flags.Timeout) * time.Millisecond
	ctx := cmdCtx
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if err := fn(ctx, resolved); err != nil {
		output.WriteError(output.Stderr(), err)
		return nil
	}
	return nil
}

func resolve(flags *GlobalFlags) (*resolvedContext, error) {
	cfg := config.Read()
	profileName := firstNonEmpty(flags.Profile, os.Getenv("AGENT_POSTHOG_PROFILE"), cfg.DefaultProfile)
	profile := config.Profile{}
	if profileName != "" {
		if found, ok := cfg.Profiles[profileName]; ok {
			profile = found
		}
	}

	host := firstNonEmpty(flags.Host, os.Getenv("AGENT_POSTHOG_BASE_URL"), profile.Host, os.Getenv("AGENT_POSTHOG_HOST"), config.DefaultHost)
	orgID := firstNonEmpty(flags.OrgID, profile.OrganizationID, os.Getenv("AGENT_POSTHOG_ORGANIZATION_ID"), os.Getenv("POSTHOG_ORGANIZATION_ID"))
	projectID := firstNonZero(flags.ProjectID, profile.ProjectID, envInt("AGENT_POSTHOG_PROJECT_ID"), envInt("POSTHOG_PROJECT_ID"))
	environmentID := firstNonZero(flags.EnvironmentID, profile.EnvironmentID, envInt("AGENT_POSTHOG_ENVIRONMENT_ID"), envInt("POSTHOG_ENVIRONMENT_ID"))
	token := firstNonEmpty(flags.APIKey, os.Getenv("POSTHOG_PERSONAL_API_KEY"), os.Getenv("AGENT_POSTHOG_PERSONAL_API_KEY"))
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

func writeItem(data any, flagFormat string) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatJSON)
	if err != nil {
		output.WriteError(output.Stderr(), err)
		return nil
	}
	output.Print(data, format, true)
	return nil
}

func writeRaw(raw json.RawMessage, flagFormat string) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatJSON)
	if err != nil {
		output.WriteError(output.Stderr(), err)
		return nil
	}
	output.WriteRawJSON(raw, format, true)
	return nil
}

func writeList(items []json.RawMessage, nextURL string, flagFormat string) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatNDJSON)
	if err != nil {
		output.WriteError(output.Stderr(), err)
		return nil
	}
	if format != output.FormatNDJSON {
		var decoded []any
		for _, raw := range items {
			var item any
			if err := json.Unmarshal(raw, &item); err == nil {
				decoded = append(decoded, item)
			}
		}
		payload := map[string]any{"results": decoded}
		if nextURL != "" {
			payload["next"] = nextURL
		}
		output.Print(payload, format, true)
		return nil
	}
	writer := output.NewNDJSONWriter(output.Stdout())
	for _, raw := range items {
		var item any
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		if err := writer.WriteItem(item); err != nil {
			return err
		}
	}
	if nextURL != "" {
		return writer.WritePagination(&output.Pagination{HasMore: true, NextURL: nextURL})
	}
	return nil
}

func baseValues(limit int) url.Values {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q
}

func envInt(name string) int {
	value, _ := strconv.Atoi(os.Getenv(name))
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

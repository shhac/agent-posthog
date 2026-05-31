package cli

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

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
	return writeRawResource(raw, flagFormat, false)
}

func writeRawResource(raw json.RawMessage, flagFormat string, full bool) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatJSON)
	if err != nil {
		output.WriteError(output.Stderr(), err)
		return nil
	}
	if !full {
		raw = compactRaw(raw)
	}
	output.WriteRawJSON(raw, format, true)
	return nil
}

func writeList(items []json.RawMessage, nextURL string, flagFormat string) error {
	return writeListResource(items, nextURL, flagFormat, false)
}

func writeListResource(items []json.RawMessage, nextURL string, flagFormat string, full bool) error {
	format, err := output.ResolveFormat(flagFormat, output.FormatNDJSON)
	if err != nil {
		output.WriteError(output.Stderr(), err)
		return nil
	}
	if !full {
		items = compactRawItems(items)
	}
	if format != output.FormatNDJSON {
		output.Print(listPayload(items, nextURL), format, true)
		return nil
	}
	return writeListNDJSON(items, nextURL)
}

func listPayload(items []json.RawMessage, nextURL string) map[string]any {
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
	return payload
}

func writeListNDJSON(items []json.RawMessage, nextURL string) error {
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

type listOptions struct {
	Limit     int
	All       bool
	PageLimit int
}

func addListPagingFlags(cmd *cobra.Command, opts *listOptions, defaultLimit int) {
	opts.Limit = defaultLimit
	opts.PageLimit = 10
	cmd.Flags().IntVar(&opts.Limit, "limit", defaultLimit, "Maximum results to request per page")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Follow pagination and stream all available pages")
	cmd.Flags().IntVar(&opts.PageLimit, "page-limit", 10, "Maximum pages to follow when --all is set")
}

func collectList(ctx context.Context, resolved *resolvedContext, path string, query url.Values, opts listOptions) ([]json.RawMessage, string, error) {
	if opts.Limit > 0 && query.Get("limit") == "" {
		query.Set("limit", strconv.Itoa(opts.Limit))
	}
	var all []json.RawMessage
	pages := 0
	for {
		page, err := resolved.Client.List(ctx, path, query)
		if err != nil {
			return nil, "", err
		}
		all = append(all, page.Results...)
		pages++
		if !opts.All || page.Next == "" {
			return all, page.Next, nil
		}
		if opts.PageLimit > 0 && pages >= opts.PageLimit {
			return all, page.Next, nil
		}
		path = page.Next
		query = url.Values{}
	}
}

func baseValues(limit int) url.Values {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q
}

func compactRawItems(items []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		out = append(out, compactRaw(item))
	}
	return out
}

func compactRaw(raw json.RawMessage) json.RawMessage {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return raw
	}
	compacted := compactValue(value)
	data, err := json.Marshal(compacted)
	if err != nil {
		return raw
	}
	return data
}

func compactValue(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = compactValue(item)
		}
		return out
	case map[string]any:
		if nested, ok := v["query_status"]; ok {
			return map[string]any{"query_status": compactValue(nested)}
		}
		keep := map[string]bool{
			"id": true, "uuid": true, "name": true, "key": true, "type": true, "event": true,
			"active": true, "archived": true, "created_at": true, "updated_at": true,
			"start_time": true, "start_date": true, "end_date": true, "last_seen_at": true,
			"distinct_ids": true, "properties": true, "email": true, "person_id": true,
			"viewed": true, "recording_duration": true, "console_error_count": true,
			"feature_flag_key": true, "rollout_percentage": true, "filters": true,
			"multivariate": true, "variants": true, "results": true, "columns": true,
			"query": true, "query_async": true, "complete": true, "error": true,
			"error_message": true, "query_progress": true, "access_token": true,
			"enabled": true, "password_required": true, "share_passwords": true,
			"detail": true, "activity": true, "scope": true, "item_id": true,
			"runtime_ms": true, "status": true, "metrics": true, "tiles": true,
		}
		out := make(map[string]any)
		for key, item := range v {
			if keep[key] {
				out[key] = compactValue(item)
			}
		}
		if len(out) == 0 {
			return v
		}
		return out
	default:
		return v
	}
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

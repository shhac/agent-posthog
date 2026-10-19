package cli

import (
	"context"
	"time"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

func registerFlagEvalCommands(flags *cobra.Command, globals *GlobalFlags) {
	flags.AddCommand(evaluateCommand(globals))
	flags.AddCommand(testCommand(globals))
	flags.AddCommand(reachCommand(globals))
}

type evalFlags struct {
	engine, flagsHost, propsJSON, groupsJSON, groupPropsJSON, deviceID string
	contexts                                                           []string
}

func (f *evalFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.engine, "engine", enginePublic, "public (/flags, as an SDK sees it; accepts property overrides) or diagnostic (stored properties only)")
	cmd.Flags().StringVar(&f.flagsHost, "flags-host", "", "Host serving /flags (default: derived from the PostHog host, or AGENT_POSTHOG_FLAGS_HOST)")
}

func (f *evalFlags) addContext(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.propsJSON, "props-json", "", `Person properties for this evaluation, e.g. '{"app_env":"staging"}' (public engine)`)
	cmd.Flags().StringVar(&f.groupsJSON, "groups-json", "", `Groups, e.g. '{"company":"acme"}'`)
	cmd.Flags().StringVar(&f.groupPropsJSON, "group-props-json", "", `Group properties, e.g. '{"company":{"plan":"pro"}}' (public engine)`)
	cmd.Flags().StringVar(&f.deviceID, "device-id", "", "Device ID, for flags bucketed by device (public engine)")
	cmd.Flags().StringArrayVar(&f.contexts, "evaluation-context", nil, "Evaluation context the request carries, as an SDK would; repeat (public engine)")
}

func evaluateCommand(globals *GlobalFlags) *cobra.Command {
	var f evalFlags
	var distinctID, at string
	cmd := &cobra.Command{
		Use:   "evaluate <id-or-key>",
		Short: "Evaluate a saved flag for one user: enabled, variant, payload, and which condition matched",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			in, err := f.context(distinctID, at)
			if err != nil {
				return err
			}
			if err := checkEngineContext(f.engine, in); err != nil {
				return err
			}
			return withClient(cmd.Context(), globals, func(ctx context.Context, resolved *resolvedContext) error {
				flag, err := currentFlag(ctx, resolved, args[0])
				if err != nil {
					return err
				}
				e, err := newEvaluator(ctx, resolved, flag, f.engine, f.flagsHost)
				if err != nil {
					return err
				}
				r := e.evaluate(ctx, in)
				if r.Status == "error" {
					return agenterrors.New("Could not evaluate the flag: "+r.Error, agenterrors.FixableByRetry).
						WithHint("This is not a false result. Retry, or try the other --engine.")
				}
				record := e.record(in, r)
				if warnings := e.warnings(); len(warnings) > 0 {
					record.Set("warnings", warnings)
				}
				return writeExact(record, globals.Format)
			})
		},
	}
	cmd.Flags().StringVar(&distinctID, "distinct-id", "", "Distinct ID to evaluate for (required; it also decides percentage rollouts)")
	cmd.Flags().StringVar(&at, "at", "", "Evaluate as of this past time, with the flag and person as they were then (--engine diagnostic)")
	f.add(cmd)
	f.addContext(cmd)
	return cmd
}

func (f *evalFlags) context(distinctID, at string) (evalContext, error) {
	if distinctID == "" {
		return evalContext{}, agenterrors.New("--distinct-id is required; it decides percentage rollouts", agenterrors.FixableByAgent)
	}
	at, err := normalizeAt(at)
	if err != nil {
		return evalContext{}, err
	}
	in := evalContext{DistinctID: distinctID, At: at, DeviceID: f.deviceID, EvaluationContexts: f.contexts}
	for _, field := range []struct {
		name, value string
		target      *map[string]any
	}{
		{"props-json", f.propsJSON, &in.PersonProps},
		{"groups-json", f.groupsJSON, &in.Groups},
		{"group-props-json", f.groupPropsJSON, &in.GroupProperties},
	} {
		if field.value == "" {
			continue
		}
		obj, err := flagspec.DecodeObject([]byte(field.value))
		if err != nil {
			return evalContext{}, agenterrors.New("--"+field.name+" must be a JSON object: "+err.Error(), agenterrors.FixableByAgent)
		}
		*field.target = obj
	}
	return in, nil
}

func checkEngineContext(engine string, in evalContext) error {
	if engine == engineDiagnostic && (in.PersonProps != nil || in.GroupProperties != nil) {
		return agenterrors.New("the diagnostic engine uses stored properties only; drop property overrides or use --engine public", agenterrors.FixableByAgent)
	}
	if engine == engineDiagnostic && (in.DeviceID != "" || len(in.EvaluationContexts) > 0) {
		return agenterrors.New("--device-id and evaluation contexts apply to /flags; use --engine public", agenterrors.FixableByAgent)
	}
	if engine != engineDiagnostic && in.At != "" {
		return agenterrors.New("evaluating as of a past moment needs --engine diagnostic, which rebuilds the flag and person as they were", agenterrors.FixableByAgent)
	}
	return nil
}

// normalizeAt checks a point in time for historical evaluation, reading a
// zoneless timestamp as UTC the way PostHog does.
func normalizeAt(at string) (string, error) {
	if at == "" {
		return "", nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, at); err == nil {
			return t.UTC().Format(time.RFC3339), nil
		}
	}
	return "", agenterrors.New("--at must be an ISO 8601 time such as 2026-04-29T15:30:00Z", agenterrors.FixableByAgent)
}

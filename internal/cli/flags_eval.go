package cli

import (
	"context"
	"os"

	"github.com/shhac/lib-agent-cli/creds"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/featureflag"
	"github.com/shhac/agent-posthog/internal/flagspec"
)

type evalFlags struct {
	engine, flagsHost, propsJSON, groupsJSON, groupPropsJSON, deviceID string
	contexts                                                           []string
}

func (f *evalFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.engine, "engine", featureflag.EnginePublic, "public (/flags, as an SDK sees it; accepts property overrides) or diagnostic (stored properties only)")
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
			if err := featureflag.CheckSubject(f.engine, in); err != nil {
				return err
			}
			return withFlags(cmd, globals, func(ctx context.Context, p featureflag.Project) error {
				e, err := f.evaluator(ctx, p, args[0])
				if err != nil {
					return err
				}
				r := e.Evaluate(ctx, in)
				if r.Status == "error" {
					return agenterrors.New("Could not evaluate the flag: "+r.Error, agenterrors.FixableByRetry).
						WithHint("This is not a false result. Retry, or try the other --engine.")
				}
				record := e.Record(in, r)
				if warnings := e.Warnings(); len(warnings) > 0 {
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

// evaluator reads the flag ref names and prepares the chosen engine.
func (f *evalFlags) evaluator(ctx context.Context, p featureflag.Project, ref string) (*featureflag.Evaluator, error) {
	flag, err := p.Fetch(ctx, ref)
	if err != nil {
		return nil, err
	}
	host := creds.FirstNonEmpty(f.flagsHost, os.Getenv("AGENT_POSTHOG_FLAGS_HOST"), featureflag.FlagsHost(p.Host))
	return p.NewEvaluator(ctx, flag, f.engine, host)
}

func (f *evalFlags) context(distinctID, at string) (featureflag.Subject, error) {
	if distinctID == "" {
		return featureflag.Subject{}, agenterrors.New("--distinct-id is required; it decides percentage rollouts", agenterrors.FixableByAgent)
	}
	at, err := featureflag.NormalizeAt(at)
	if err != nil {
		return featureflag.Subject{}, err
	}
	in := featureflag.Subject{DistinctID: distinctID, At: at, DeviceID: f.deviceID, EvaluationContexts: f.contexts}
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
			return featureflag.Subject{}, agenterrors.New("--"+field.name+" must be a JSON object: "+err.Error(), agenterrors.FixableByAgent)
		}
		*field.target = obj
	}
	return in, nil
}

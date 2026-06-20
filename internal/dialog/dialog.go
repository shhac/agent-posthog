// Package dialog delegates the native secret-entry boilerplate to
// lib-agent-cli/dialog; this thin wrapper keeps agent-posthog's existing
// PromptSecret signature (with an initial value to edit). (Migration shim.)
package dialog

import (
	"context"

	clidialog "github.com/shhac/lib-agent-cli/dialog"

	agenterrors "github.com/shhac/agent-posthog/internal/errors"
)

// PromptSecret opens a masked native prompt seeded with initial, so a token
// never transits argv. The returned error is the lib's neutral error;
// classify it with [Classify] to build a structured envelope.
func PromptSecret(ctx context.Context, title, label, initial string) (string, error) {
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

// Classify maps a dialog error onto agent-posthog's fixable_by taxonomy and a
// hint, so a cancelled prompt surfaces as retry and a headless host as human.
// The lib returns neutral errors now (not output.Error), so call sites run this
// to preserve the structured {error,fixable_by,hint} output.
func Classify(err error) (agenterrors.FixableBy, string) {
	cat, hint := clidialog.ClassifyError(err)
	switch cat {
	case clidialog.CategoryHuman:
		return agenterrors.FixableByHuman, hint
	case clidialog.CategoryRetry:
		return agenterrors.FixableByRetry, hint
	default:
		return agenterrors.FixableByAgent, hint
	}
}

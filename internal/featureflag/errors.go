package featureflag

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/shhac/lib-agent-cli/creds"
	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/api"
)

// ClassifyWriteError separates approval-policy refusals (a human must
// approve) from stale-version conflicts (re-read and retry), which share HTTP
// 409.
func ClassifyWriteError(err error) error {
	var httpErr *api.HTTPError
	if !errors.As(err, &httpErr) {
		return err
	}
	var body struct {
		Code            string `json:"code"`
		Detail          string `json:"detail"`
		Error           string `json:"error"`
		ChangeRequestID string `json:"change_request_id"`
		Guidance        string `json:"guidance"`
	}
	_ = json.Unmarshal(httpErr.Body, &body)
	switch {
	case httpErr.Status == http.StatusConflict && (body.Code == "approval_required" || body.Code == "change_request_pending"):
		return agenterrors.New(fmt.Sprintf("Not applied: an approval policy gates this change (change request %s)", body.ChangeRequestID), agenterrors.FixableByHuman).
			WithHint("Ask an approver to review the change request in PostHog. Nothing was written.")
	case httpErr.Status == http.StatusConflict:
		return agenterrors.New("Not applied: "+creds.FirstNonEmpty(body.Detail, body.Error, "the flag changed since it was read"), agenterrors.FixableByAgent).
			WithHint("Re-read the flag with 'agent-posthog flags get <ref>' and decide the change against its current definition.")
	case body.Code == "policy_conflict":
		return agenterrors.New("Not applied: "+creds.FirstNonEmpty(body.Error, body.Detail, "the change matches more than one approval policy"), agenterrors.FixableByHuman).
			WithHint(creds.FirstNonEmpty(body.Guidance, "Split the change so each approval policy applies on its own."))
	default:
		return err
	}
}

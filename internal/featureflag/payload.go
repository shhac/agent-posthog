package featureflag

import (
	"context"
	"fmt"

	agenterrors "github.com/shhac/lib-agent-output"

	"github.com/shhac/agent-posthog/internal/flagspec"
)

// Payload is the payload a matching user receives: per variant for
// multivariate flags (variant must name one), the single payload otherwise.
type Payload struct {
	Key any
	// Slot is the filters.payloads key holding it: the variant, or "true".
	Slot  string
	Set   bool
	Value any
}

// ReadPayload decodes the flag's payload for variant.
func ReadPayload(flag map[string]any, variant string) (*Payload, error) {
	slot, err := payloadSlot(flag, variant)
	if err != nil {
		return nil, err
	}
	stored, ok := flagspec.Object(flagspec.Filters(flag)["payloads"])[slot]
	payload := &Payload{Key: flag["key"], Slot: slot, Set: ok}
	if !ok {
		return payload, nil
	}
	value, err := flagspec.DecodePayload(stored)
	if err != nil {
		return nil, agenterrors.Wrap(err, agenterrors.FixableByHuman)
	}
	payload.Value = value
	return payload, nil
}

// SetPayload sets the payload for variant to value, keeping PostHog's stored
// encoding when the value is unchanged.
func SetPayload(variant string, value any) Build {
	return payloadEdit(variant, func(payloads map[string]any, slot string) error {
		encoded, err := flagspec.KeepOrEncode(payloads[slot], value)
		payloads[slot] = encoded
		return err
	})
}

// UnsetPayload removes the payload for variant.
func UnsetPayload(variant string) Build {
	return payloadEdit(variant, func(payloads map[string]any, slot string) error {
		delete(payloads, slot)
		return nil
	})
}

func payloadEdit(variant string, edit func(payloads map[string]any, slot string) error) Build {
	return func(_ context.Context, _ Project, flag map[string]any) (*flagspec.Patch, []string, error) {
		if flag["has_encrypted_payloads"] == true {
			return nil, nil, fmt.Errorf("this flag has encrypted payloads, which agent-posthog does not change; edit them in PostHog")
		}
		if err := flagspec.RequireV1(flagspec.Filters(flag)); err != nil {
			return nil, nil, err
		}
		slot, err := payloadSlot(flag, variant)
		if err != nil {
			return nil, nil, err
		}
		payloads, _ := flagspec.Clone(flagspec.Object(flagspec.Filters(flag)["payloads"])).(map[string]any)
		if payloads == nil {
			payloads = map[string]any{}
		}
		if err := edit(payloads, slot); err != nil {
			return nil, nil, err
		}
		patch := flagspec.NewPatch()
		patch.Filters["payloads"] = payloads
		return patch, nil, nil
	}
}

// payloadSlot maps a variant choice to the filters.payloads key: the variant
// for multivariate flags, "true" otherwise.
func payloadSlot(flag map[string]any, variant string) (string, error) {
	if flagspec.TypeOf(flag) != flagspec.TypeMultivariate {
		if variant != "" {
			return "", agenterrors.New("only multivariate flags have per-variant payloads; drop --variant", agenterrors.FixableByAgent)
		}
		return flagspec.BooleanPayloadKey, nil
	}
	if variant == "" {
		return "", agenterrors.New("multivariate flags have one payload per variant; pass --variant", agenterrors.FixableByAgent)
	}
	if !flagspec.VariantKeys(flag)[variant] {
		return "", agenterrors.New("the flag has no variant "+variant, agenterrors.FixableByAgent)
	}
	return variant, nil
}

package dialog

import (
	"context"
	"fmt"

	"github.com/ncruces/zenity"
)

func PromptSecret(ctx context.Context, title, label, initial string) (string, error) {
	value, err := zenity.Entry(
		label,
		zenity.Title(title),
		zenity.EntryText(initial),
		zenity.HideText(),
		zenity.Context(ctx),
	)
	if err != nil {
		return "", fmt.Errorf("prompt for secret: %w", err)
	}
	return value, nil
}

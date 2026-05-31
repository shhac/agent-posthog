package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/output"
)

func registerCompletion(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:       "completion <bash|zsh|fish|powershell>",
		Short:     "Generate a shell completion script",
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		Long: `Generate a shell completion script for the given shell.

Cobra emits the actual completion script. The script supports
tab-completion for command names and flags.

Setup:

  # bash
  agent-posthog completion bash > ~/.local/share/bash-completion/completions/agent-posthog

  # zsh
  agent-posthog completion zsh > ~/.zsh/completions/_agent-posthog

  # fish
  agent-posthog completion fish > ~/.config/fish/completions/agent-posthog.fish

  # powershell
  agent-posthog completion powershell | Out-String | Invoke-Expression`,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := output.Stdout()
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(w, true)
			case "zsh":
				return cmd.Root().GenZshCompletion(w)
			case "fish":
				return cmd.Root().GenFishCompletion(w, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(w)
			}
			return fmt.Errorf("unsupported shell: %s", args[0])
		},
	})
}

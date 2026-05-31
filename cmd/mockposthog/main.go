package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-posthog/internal/mockposthog"
)

func main() {
	var addr string
	var routes bool

	cmd := &cobra.Command{
		Use:   "mockposthog",
		Short: "Local mock PostHog API server for agent-posthog tests",
		Long:  "Local mock PostHog API server for agent-posthog tests.\n\nRoutes:\n" + routeHelp(),
		RunE: func(cmd *cobra.Command, args []string) error {
			if routes {
				for _, line := range mockposthog.Routes() {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), line)
				}
				return nil
			}
			server := &http.Server{
				Addr:    addr,
				Handler: mockposthog.NewServer(),
			}
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"status":   "listening",
				"base_url": "http://" + addr,
			})
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:18118", "Address to listen on")
	cmd.Flags().BoolVar(&routes, "routes", false, "Print mock route map and exit")

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func routeHelp() string {
	out := ""
	for _, line := range mockposthog.Routes() {
		out += "  " + line + "\n"
	}
	return out
}

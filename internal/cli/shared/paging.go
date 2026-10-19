package shared

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

type listOptions struct {
	Limit     int
	All       bool
	PageLimit int
}

func addListPagingFlags(cmd *cobra.Command, opts *listOptions, defaultLimit int) {
	cmd.Flags().IntVar(&opts.Limit, "limit", defaultLimit, "Maximum results to request per page")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Follow pagination and stream all available pages")
	cmd.Flags().IntVar(&opts.PageLimit, "page-limit", 10, "Maximum pages to follow when --all is set")
}

func collectList(ctx context.Context, resolved *Resolved, path string, query url.Values, opts listOptions) ([]json.RawMessage, string, error) {
	if opts.Limit > 0 && query.Get("limit") == "" {
		query.Set("limit", strconv.Itoa(opts.Limit))
	}
	pages := 1
	if opts.All {
		pages = opts.PageLimit
	}
	return resolved.Client.ListAll(ctx, path, query, pages)
}

func BaseValues(limit int) url.Values {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q
}

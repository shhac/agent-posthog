package cli

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

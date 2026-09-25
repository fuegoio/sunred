package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/fuegoio/sunred/go/sdk/sunred"
	"github.com/spf13/cobra"
)

// historyMaxLimit matches the API's `limit` maximum on GET /v1/history; a
// larger --limit is clamped instead of failing with a 422.
const historyMaxLimit = 200

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "List your read history, most recently read first",
	Run: func(cmd *cobra.Command, _ []string) {
		_, c := mustClient()
		limit, err := cmd.Flags().GetInt64("limit")
		if err != nil || limit <= 0 {
			limit = 50
		}
		if err := runHistory(os.Stdout, c, limit); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	},
}

// runHistory fetches the read history and writes a tab-separated table
// (ID, read time, feed, title) to w, most recently read first. limit is
// clamped to historyMaxLimit; anything <= 0 falls back to the API default.
func runHistory(w io.Writer, c *sunred.ClientWithResponses, limit int64) error {
	if limit <= 0 {
		limit = 50
	}
	if limit > historyMaxLimit {
		limit = historyMaxLimit
	}
	resp, err := c.ListHistoryWithResponse(context.Background(), &sunred.ListHistoryParams{
		Limit: &limit,
	})
	if err != nil {
		return err
	}
	if resp.JSON200 == nil {
		return fmt.Errorf("API error %s", formatAPIError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault))
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tREAD AT\tFEED\tTITLE")
	for _, e := range *resp.JSON200 {
		feed := ""
		if e.Feed != nil {
			feed = e.Feed.Title
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", e.Id, e.ChangedAt.Format("2006-01-02 15:04"), feed, e.Title)
	}
	return tw.Flush()
}

func init() {
	historyCmd.Flags().Int64("limit", 50, "maximum number of articles to list (capped at 200)")
	rootCmd.AddCommand(historyCmd)
}

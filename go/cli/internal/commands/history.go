package commands

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/fuegoio/sunred/go/sdk/sunred"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "List your read history, most recently read first",
	Run: func(cmd *cobra.Command, _ []string) {
		_, c := mustClient()
		limit := int64(50)
		if v, err := cmd.Flags().GetInt64("limit"); err == nil && v > 0 {
			limit = v
		}
		resp, err := c.ListHistoryWithResponse(context.Background(), &sunred.ListHistoryParams{
			Limit: &limit,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.JSON200 == nil {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tREAD AT\tFEED\tTITLE")
		for _, e := range *resp.JSON200 {
			feed := ""
			if e.Feed != nil {
				feed = e.Feed.Title
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", e.Id, e.ChangedAt.Format("2006-01-02 15:04"), feed, e.Title)
		}
		w.Flush()
	},
}

func init() {
	historyCmd.Flags().Int64("limit", 50, "maximum number of articles to list")
	rootCmd.AddCommand(historyCmd)
}

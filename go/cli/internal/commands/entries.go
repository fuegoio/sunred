package commands

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/fuegoio/sunred/go/sdk/sunred"
	"github.com/spf13/cobra"
)

var entriesCmd = &cobra.Command{
	Use:   "entries",
	Short: "Browse feed entries",
}

var entriesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List unread entries",
	Run: func(cmd *cobra.Command, _ []string) {
		_, c := mustClient()
		limit := int64(50)
		status := sunred.ListEntriesParamsStatusUnread
		resp, err := c.ListEntriesWithResponse(context.Background(), &sunred.ListEntriesParams{
			Limit:  &limit,
			Status: &status,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.JSON200 == nil {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}

		// Build a feed-name lookup from the feeds endpoint.
		feedNames := map[int64]string{}
		if fr, ferr := c.ListFeedsWithResponse(context.Background()); ferr == nil && fr.JSON200 != nil {
			for _, f := range *fr.JSON200 {
				feedNames[f.Id] = f.Title
			}
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTARRED\tSTATUS\tDATE\tFEED\tTITLE")
		for _, e := range *resp.JSON200 {
			star := " "
			if e.Starred {
				star = "★"
			}
			status := e.Status
			date := e.PublishedAt.Format("2006-01-02")
			feed := feedNames[e.FeedId]
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", e.Id, star, status, date, feed, e.Title)
		}
		w.Flush()
	},
}

var entriesReadCmd = &cobra.Command{
	Use:   "read <id>",
	Short: "Read a single entry",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		id, err := parseInt64(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid id: %v\n", err)
			os.Exit(1)
		}
		resp, err := c.GetEntryWithResponse(context.Background(), id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.JSON200 == nil {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		e := *resp.JSON200
		fmt.Printf("Title: %s\n", e.Title)
		if e.Author != nil {
			fmt.Printf("Author: %s\n", *e.Author)
		}
		fmt.Printf("URL: %s\n", e.Url)
		fmt.Printf("Published: %s\n", e.PublishedAt.Format("2006-01-02 15:04"))
		fmt.Printf("Status: %s\n\n", e.Status)
		if e.Description != nil {
			fmt.Println(*e.Description)
		}
	},
}

var entriesMarkCmd = &cobra.Command{
	Use:   "mark <id> <read|unread|removed>",
	Short: "Set the read status of an entry by id",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		id, err := parseInt64(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid id: %v\n", err)
			os.Exit(1)
		}
		status, err := parseEntriesStatus(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		ids := []int64{id}
		resp, err := c.UpdateEntriesWithResponse(context.Background(), sunred.UpdateEntriesRequest{
			EntryIds: &ids,
			Status:   status,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.StatusCode() != 204 {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		fmt.Printf("Marked entry %d as %s\n", id, status)
	},
}

var entriesStarCmd = &cobra.Command{
	Use:   "star <id> <on|off>",
	Short: "Star or unstar an entry by id",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		id, err := parseInt64(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid id: %v\n", err)
			os.Exit(1)
		}
		starred, err := parseBoolArg(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		resp, err := c.ToggleEntryStarredWithResponse(context.Background(), id, sunred.ToggleEntryStarredRequest{
			Starred: starred,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.StatusCode() != 204 {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		verb := "Unstarred"
		if starred {
			verb = "Starred"
		}
		fmt.Printf("%s entry %d\n", verb, id)
	},
}

var entriesMarkByUrlCmd = &cobra.Command{
	Use:   "mark-by-url <url> <read|unread|removed>",
	Short: "Set the read status of an article by URL (no entry id required)",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		status, err := parseEntryStatusByUrl(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		resp, err := c.UpdateEntryStatusByUrlWithResponse(context.Background(), sunred.UpdateEntryStatusByUrlRequest{
			ArticleUrl: args[0],
			Status:     status,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.StatusCode() != 204 {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		fmt.Printf("Marked %s as %s\n", args[0], status)
	},
}

var entriesStarByUrlCmd = &cobra.Command{
	Use:   "star-by-url <url> <title> <on|off>",
	Short: "Star or unstar an article by URL (no entry id required)",
	Args:  cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		starred, err := parseBoolArg(args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		body := sunred.ToggleEntryStarredByUrlRequest{
			ArticleUrl: args[0],
			Title:      args[1],
			Starred:    starred,
		}
		if v, err := cmd.Flags().GetString("description"); err == nil && v != "" {
			body.Description = ptr(v)
		}
		if v, err := cmd.Flags().GetString("author"); err == nil && v != "" {
			body.Author = ptr(v)
		}
		if v, err := cmd.Flags().GetString("feed-url"); err == nil && v != "" {
			body.FeedUrl = ptr(v)
		}
		if v, err := cmd.Flags().GetString("feed-title"); err == nil && v != "" {
			body.FeedTitle = ptr(v)
		}
		if v, err := cmd.Flags().GetString("feed-site-url"); err == nil && v != "" {
			body.FeedSiteUrl = ptr(v)
		}
		resp, err := c.ToggleEntryStarredByUrlWithResponse(context.Background(), body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.StatusCode() != 204 {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		verb := "Unstarred"
		if starred {
			verb = "Starred"
		}
		fmt.Printf("%s %s\n", verb, args[0])
	},
}

func init() {
	entriesStarByUrlCmd.Flags().String("description", "", "article description")
	entriesStarByUrlCmd.Flags().String("author", "", "article author")
	entriesStarByUrlCmd.Flags().String("feed-url", "", "feed URL")
	entriesStarByUrlCmd.Flags().String("feed-title", "", "feed title")
	entriesStarByUrlCmd.Flags().String("feed-site-url", "", "feed site URL")

	entriesCmd.AddCommand(entriesListCmd, entriesReadCmd, entriesMarkCmd, entriesStarCmd,
		entriesMarkByUrlCmd, entriesStarByUrlCmd)
	rootCmd.AddCommand(entriesCmd)
}

package commands

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/fuegoio/sunred/go/sdk/sunred"
	"github.com/spf13/cobra"
)

var feedsCmd = &cobra.Command{
	Use:   "feeds",
	Short: "Manage feed subscriptions",
}

var feedsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all feeds",
	Run: func(cmd *cobra.Command, _ []string) {
		_, c := mustClient()
		resp, err := c.ListFeedsWithResponse(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.JSON200 == nil {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tTITLE\tURL\tSITE")
		for _, f := range *resp.JSON200 {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", f.Id, f.Title, f.FeedUrl, f.SiteUrl)
		}
		w.Flush()
	},
}

var feedsAddCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Subscribe to a feed",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		resp, err := c.CreateFeedWithResponse(context.Background(), sunred.CreateFeedJSONRequestBody{
			FeedUrl: args[0],
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.JSON200 == nil {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		f := *resp.JSON200
		fmt.Printf("Subscribed to feed %d: %s\n", f.Id, f.Title)
	},
}

var feedsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Unsubscribe from a feed",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		id, err := parseInt64(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid id: %v\n", err)
			os.Exit(1)
		}
		resp, err := c.DeleteFeedWithResponse(context.Background(), id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.StatusCode() != 204 {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		fmt.Printf("Deleted feed %d\n", id)
	},
}

var feedsPreviewCmd = &cobra.Command{
	Use:   "preview <url>",
	Short: "Preview a feed without subscribing (discovery)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		resp, err := c.PreviewFeedWithResponse(context.Background(), sunred.PreviewFeedJSONRequestBody{
			FeedUrl: args[0],
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.JSON200 == nil {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		p := *resp.JSON200
		fmt.Printf("Title: %s\n", p.Title)
		if p.Description != nil && *p.Description != "" {
			fmt.Printf("Description: %s\n", *p.Description)
		}
		fmt.Printf("Site: %s\n", p.SiteUrl)
		fmt.Printf("Feed: %s\n", p.FeedUrl)
		if p.Subscribers != nil {
			fmt.Printf("Subscribers: %d (global: %d)\n", p.Subscribers.Count, p.Subscribers.GlobalCount)
		} else {
			fmt.Println("Subscribers: 0 (not yet subscribed on this instance)")
		}
		fmt.Println()
		if p.Items == nil || len(*p.Items) == 0 {
			fmt.Println("No preview items.")
			return
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "STAR\tREAD\tDATE\tTITLE")
		for _, it := range *p.Items {
			star := " "
			if it.Starred != nil && *it.Starred {
				star = "★"
			}
			read := " "
			if it.Status != nil && *it.Status == "read" {
				read = "✓"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", star, read, it.PublishedAt.Format("2006-01-02"), it.Title)
		}
		w.Flush()
	},
}

var feedsSubscribersCmd = &cobra.Command{
	Use:   "subscribers <id>",
	Short: "Show subscriber count and list for a feed",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		id, err := parseInt64(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid id: %v\n", err)
			os.Exit(1)
		}
		resp, err := c.FeedSubscribersWithResponse(context.Background(), id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.JSON200 == nil {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		s := *resp.JSON200
		fmt.Printf("Subscribers: %d (global: %d)\n\n", s.Count, s.GlobalCount)
		if s.Subscribers == nil || len(*s.Subscribers) == 0 {
			return
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "HANDLE\tNAME")
		for _, u := range *s.Subscribers {
			name := ""
			if u.DisplayName != nil {
				name = *u.DisplayName
			}
			fmt.Fprintf(w, "%s\t%s\n", u.Handle, name)
		}
		w.Flush()
	},
}

var feedsRefreshCmd = &cobra.Command{
	Use:   "refresh <id>",
	Short: "Refresh a feed (fetch new entries now)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		id, err := parseInt64(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid id: %v\n", err)
			os.Exit(1)
		}
		resp, err := c.RefreshFeedWithResponse(context.Background(), id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.StatusCode() != 204 {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		fmt.Printf("Refreshed feed %d\n", id)
	},
}

var feedsMarkReadCmd = &cobra.Command{
	Use:   "mark-read <id>",
	Short: "Mark all entries in a feed as read",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		id, err := parseInt64(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid id: %v\n", err)
			os.Exit(1)
		}
		resp, err := c.MarkFeedReadWithResponse(context.Background(), id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.StatusCode() != 204 {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		fmt.Printf("Marked feed %d as read\n", id)
	},
}

var feedsRenameCmd = &cobra.Command{
	Use:   "rename <id> <title>",
	Short: "Rename a feed",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		id, err := parseInt64(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid id: %v\n", err)
			os.Exit(1)
		}
		resp, err := c.UpdateFeedWithResponse(context.Background(), id, sunred.UpdateFeedJSONRequestBody{
			Title: ptr(args[1]),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.JSON200 == nil {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		fmt.Printf("Renamed feed %d to %s\n", id, args[1])
	},
}

var feedsMoveCmd = &cobra.Command{
	Use:   "move <id> <folder-id>",
	Short: "Move a feed to a folder (use 0 to move to the top level)",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		_, c := mustClient()
		id, err := parseInt64(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid id: %v\n", err)
			os.Exit(1)
		}
		folderID, err := parseInt64(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: invalid folder id: %v\n", err)
			os.Exit(1)
		}
		body := sunred.UpdateFeedJSONRequestBody{}
		if folderID == 0 {
			body.FolderId = ptr(int64(0))
		} else {
			body.FolderId = ptr(folderID)
		}
		resp, err := c.UpdateFeedWithResponse(context.Background(), id, body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.JSON200 == nil {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		fmt.Printf("Moved feed %d to folder %s\n", id, args[1])
	},
}

func init() {
	feedsCmd.AddCommand(feedsListCmd, feedsAddCmd, feedsDeleteCmd,
		feedsPreviewCmd, feedsSubscribersCmd, feedsRefreshCmd,
		feedsMarkReadCmd, feedsRenameCmd, feedsMoveCmd)
	rootCmd.AddCommand(feedsCmd)
}

package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var meCmd = &cobra.Command{
	Use:   "me",
	Short: "Show current user",
	Run: func(cmd *cobra.Command, _ []string) {
		_, c := mustClient()
		resp, err := c.GetMeWithResponse(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if resp.JSON200 == nil {
			printError(resp.HTTPResponse.StatusCode, resp.ApplicationproblemJSONDefault)
			os.Exit(1)
		}
		u := *resp.JSON200
		fmt.Printf("ID: %d\n", u.Id)
		fmt.Printf("Handle: %s\n", u.Handle)
		if u.DisplayName != nil {
			fmt.Printf("Name: %s\n", *u.DisplayName)
		}
		if u.Bio != nil {
			fmt.Printf("Bio: %s\n", *u.Bio)
		}
		fmt.Printf("PDS sync: %s\n", u.PdsSyncStatus)
		fmt.Printf("Created: %s\n", u.CreatedAt.Format("2006-01-02 15:04"))
	},
}

func init() {
	rootCmd.AddCommand(meCmd)
}

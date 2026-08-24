package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/fuegoio/sunred/go/sdk/sunred"
)

// parseInt64 parses a string to int64, exiting on error.
func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }

// parseBoolArg accepts on/off, true/false, 1/0 (case-insensitive).
func parseBoolArg(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "on", "true", "1":
		return true, nil
	case "off", "false", "0":
		return false, nil
	}
	return false, fmt.Errorf("expected on/off, got %q", s)
}

// parseEntriesStatus parses a status for the by-id UpdateEntries endpoint.
func parseEntriesStatus(s string) (sunred.UpdateEntriesRequestStatus, error) {
	switch strings.ToLower(s) {
	case "read":
		return sunred.UpdateEntriesRequestStatusRead, nil
	case "unread":
		return sunred.UpdateEntriesRequestStatusUnread, nil
	case "removed":
		return sunred.UpdateEntriesRequestStatusRemoved, nil
	}
	return "", fmt.Errorf("invalid status %q (want read, unread, or removed)", s)
}

// parseEntryStatusByUrl parses a status for the by-URL UpdateEntryStatusByUrl endpoint.
func parseEntryStatusByUrl(s string) (sunred.UpdateEntryStatusByUrlRequestStatus, error) {
	switch strings.ToLower(s) {
	case "read":
		return sunred.UpdateEntryStatusByUrlRequestStatusRead, nil
	case "unread":
		return sunred.UpdateEntryStatusByUrlRequestStatusUnread, nil
	case "removed":
		return sunred.UpdateEntryStatusByUrlRequestStatusRemoved, nil
	}
	return "", fmt.Errorf("invalid status %q (want read, unread, or removed)", s)
}

// printError prints an API error response to stderr.
func printError(status int, errModel *sunred.ErrorModel) {
	if errModel != nil && errModel.Detail != nil {
		fmt.Fprintf(os.Stderr, "error (%d): %s\n", status, *errModel.Detail)
		return
	}
	if errModel != nil && errModel.Title != nil {
		fmt.Fprintf(os.Stderr, "error (%d): %s\n", status, *errModel.Title)
		return
	}
	fmt.Fprintf(os.Stderr, "error (%d)\n", status)
}

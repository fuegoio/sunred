package atproto

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is a thin AT Proto XRPC client bound to a specific PDS base URL
// and an optional bearer access token.
type Client struct {
	pdsURL     string
	httpClient *http.Client
	token      string // Bearer access token; empty for unauthenticated calls
}

// NewClient returns a Client for the given PDS. token may be empty for
// unauthenticated read-only calls.
func NewClient(pdsURL, token string) *Client {
	return &Client{
		pdsURL: pdsURL,
		token:  token,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// PDSURL returns the PDS base URL this client is bound to.
func (c *Client) PDSURL() string { return c.pdsURL }

// xrpcError is an XRPC error response body.
type xrpcError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (e *xrpcError) err() error {
	if e.Message != "" {
		return fmt.Errorf("%s: %s", e.Error, e.Message)
	}
	return fmt.Errorf("%s", e.Error)
}

// Query performs an XRPC GET (query) call.
func (c *Client) Query(ctx context.Context, lexiconID string, params map[string]string, out any) error {
	u, err := url.Parse(c.pdsURL)
	if err != nil {
		return fmt.Errorf("parse pds url: %w", err)
	}
	u.Path = "/xrpc/" + lexiconID

	if len(params) > 0 {
		q := u.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("xrpc query %s: %w", lexiconID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var xe xrpcError
		if err := json.Unmarshal(body, &xe); err == nil && xe.Error != "" {
			return xe.err()
		}
		return fmt.Errorf("xrpc query %s: http %d", lexiconID, resp.StatusCode)
	}

	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// Procedure performs an XRPC POST (procedure) call.
func (c *Client) Procedure(ctx context.Context, lexiconID string, input, out any) error {
	u, err := url.Parse(c.pdsURL)
	if err != nil {
		return fmt.Errorf("parse pds url: %w", err)
	}
	u.Path = "/xrpc/" + lexiconID

	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("marshal input: %w", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), body)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("xrpc procedure %s: %w", lexiconID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var xe xrpcError
		if err := json.Unmarshal(respBody, &xe); err == nil && xe.Error != "" {
			return xe.err()
		}
		return fmt.Errorf("xrpc procedure %s: http %d", lexiconID, resp.StatusCode)
	}

	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// --- Session management ---

// CreateSessionInput is the body for com.atproto.server.createSession.
type CreateSessionInput struct {
	Identifier string `json:"identifier"` // handle or DID
	Password   string `json:"password"`
}

// Session holds the tokens returned by createSession / refreshSession.
type Session struct {
	AccessJwt  string `json:"accessJwt"`
	RefreshJwt string `json:"refreshJwt"`
	DID        string `json:"did"`
	Handle     string `json:"handle"`
}

// CreateSession authenticates and returns a new session.
func (c *Client) CreateSession(ctx context.Context, identifier, password string) (*Session, error) {
	var s Session
	if err := c.Procedure(ctx, "com.atproto.server.createSession", CreateSessionInput{
		Identifier: identifier,
		Password:   password,
	}, &s); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &s, nil
}

// RefreshSession exchanges a refresh token for a new session.
func (c *Client) RefreshSession(ctx context.Context, refreshToken string) (*Session, error) {
	refreshClient := NewClient(c.pdsURL, refreshToken)
	var s Session
	if err := refreshClient.Procedure(ctx, "com.atproto.server.refreshSession", nil, &s); err != nil {
		return nil, fmt.Errorf("refresh session: %w", err)
	}
	return &s, nil
}

// --- Record operations ---

// PutRecordInput is the body for com.atproto.repo.putRecord.
type PutRecordInput struct {
	Repo       string `json:"repo"`       // DID
	Collection string `json:"collection"` // lexicon ID
	Rkey       string `json:"rkey"`       // record key
	Record     any    `json:"record"`
}

// PutRecordOutput is the response from putRecord.
type PutRecordOutput struct {
	URI string `json:"uri"`
	CID string `json:"cid"`
}

// PutRecord writes (creates or replaces) a record in the repo.
func (c *Client) PutRecord(ctx context.Context, did, collection, rkey string, record any) (*PutRecordOutput, error) {
	var out PutRecordOutput
	if err := c.Procedure(ctx, "com.atproto.repo.putRecord", PutRecordInput{
		Repo:       did,
		Collection: collection,
		Rkey:       rkey,
		Record:     record,
	}, &out); err != nil {
		return nil, fmt.Errorf("put record %s/%s: %w", collection, rkey, err)
	}
	return &out, nil
}

// DeleteRecordInput is the body for com.atproto.repo.deleteRecord.
type DeleteRecordInput struct {
	Repo       string `json:"repo"`
	Collection string `json:"collection"`
	Rkey       string `json:"rkey"`
}

// DeleteRecord removes a record from the repo. No-ops on missing records.
func (c *Client) DeleteRecord(ctx context.Context, did, collection, rkey string) error {
	return c.Procedure(ctx, "com.atproto.repo.deleteRecord", DeleteRecordInput{
		Repo:       did,
		Collection: collection,
		Rkey:       rkey,
	}, nil)
}

// ListRecordsOutput is the response from com.atproto.repo.listRecords.
type ListRecordsOutput struct {
	Records []struct {
		URI   string          `json:"uri"`
		CID   string          `json:"cid"`
		Value json.RawMessage `json:"value"`
	} `json:"records"`
	Cursor string `json:"cursor"`
}

// GetRecordOutput is the response from com.atproto.repo.getRecord.
type GetRecordOutput struct {
	URI   string          `json:"uri"`
	CID   string          `json:"cid"`
	Value json.RawMessage `json:"value"`
}

// GetRecord fetches a single record by collection + rkey from a repo. Returns
// an error when the record does not exist; callers should treat that as "no
// profile set" rather than a hard failure.
func (c *Client) GetRecord(ctx context.Context, did, collection, rkey string) (*GetRecordOutput, error) {
	params := map[string]string{
		"repo":       did,
		"collection": collection,
		"rkey":       rkey,
	}
	var out GetRecordOutput
	if err := c.Query(ctx, "com.atproto.repo.getRecord", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRecords returns records in a collection, newest first.
func (c *Client) ListRecords(ctx context.Context, did, collection string, limit int, cursor string) (*ListRecordsOutput, error) {
	params := map[string]string{
		"repo":       did,
		"collection": collection,
		"limit":      fmt.Sprintf("%d", limit),
	}
	if cursor != "" {
		params["cursor"] = cursor
	}
	var out ListRecordsOutput
	if err := c.Query(ctx, "com.atproto.repo.listRecords", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

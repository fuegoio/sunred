package atproto

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
)

// captured holds the last put/delete call received by the mock PDS.
type captured struct {
	Repo       string
	Collection string
	Rkey       string
	Value      map[string]any
}

// writerMock returns a PDS URL whose /xrpc/com.atproto.repo.{put,delete}Record
// handlers record the last call into cap.
func writerMock(t *testing.T) (pdsURL string, cap *captured) {
	t.Helper()
	cap = &captured{}
	mux := http.NewServeMux()
	mux.HandleFunc("/xrpc/com.atproto.repo.putRecord", func(w http.ResponseWriter, r *http.Request) {
		var in PutRecordInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cap.Repo = in.Repo
		cap.Collection = in.Collection
		cap.Rkey = in.Rkey
		b, _ := json.Marshal(in.Record)
		_ = json.Unmarshal(b, &cap.Value)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uri":"at://did/col/rkey","cid":"bafy"}`))
	})
	mux.HandleFunc("/xrpc/com.atproto.repo.deleteRecord", func(w http.ResponseWriter, r *http.Request) {
		var in DeleteRecordInput
		_ = json.NewDecoder(r.Body).Decode(&in)
		cap.Repo = in.Repo
		cap.Collection = in.Collection
		cap.Rkey = in.Rkey
		w.WriteHeader(http.StatusOK)
	})
	srv := mockPDS(t, mux)
	return srv.URL, cap
}

func TestWriter_PutFollow(t *testing.T) {
	pdsURL, cap := writerMock(t)
	w := NewWriter(atclient.NewAPIClient(pdsURL), "did:plc:alice")
	rkey, err := w.PutFollow(context.Background(), "did:plc:bob")
	if err != nil {
		t.Fatalf("PutFollow: %v", err)
	}
	if len(rkey) != 13 {
		t.Errorf("rkey length=%d, want 13", len(rkey))
	}
	if cap.Collection != CollectionFollow {
		t.Errorf("collection=%q, want %q", cap.Collection, CollectionFollow)
	}
	if cap.Value["subject"] != "did:plc:bob" {
		t.Errorf("subject=%v, want 'did:plc:bob'", cap.Value["subject"])
	}
}

func TestWriter_DeleteFollow(t *testing.T) {
	pdsURL, cap := writerMock(t)
	w := NewWriter(atclient.NewAPIClient(pdsURL), "did:plc:alice")
	err := w.DeleteFollow(context.Background(), "rkey-to-delete")
	if err != nil {
		t.Fatalf("DeleteFollow: %v", err)
	}
	if cap.Rkey != "rkey-to-delete" {
		t.Errorf("deleted rkey=%q, want 'rkey-to-delete'", cap.Rkey)
	}
	if cap.Collection != CollectionFollow {
		t.Errorf("collection=%q, want %q", cap.Collection, CollectionFollow)
	}
}

func TestWriter_PutShare(t *testing.T) {
	pdsURL, cap := writerMock(t)
	w := NewWriter(atclient.NewAPIClient(pdsURL), "did:plc:alice")
	pub := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	rkey, err := w.PutShare(context.Background(),
		"https://example.com/article",
		"Test Article", "A test", "https://feed.example.com", "Feed Title",
		"https://example.com", "Author Name", &pub, time.Now(),
	)
	if err != nil {
		t.Fatalf("PutShare: %v", err)
	}
	if len(rkey) != 13 {
		t.Errorf("rkey length=%d, want 13", len(rkey))
	}
	if cap.Collection != CollectionShare {
		t.Errorf("collection=%q, want %q", cap.Collection, CollectionShare)
	}
	if cap.Value["articleUrl"] != "https://example.com/article" {
		t.Errorf("articleUrl=%v", cap.Value["articleUrl"])
	}
	if cap.Value["feedTitle"] != "Feed Title" {
		t.Errorf("feedTitle=%v", cap.Value["feedTitle"])
	}
	if cap.Value["publishedAt"] == "" || cap.Value["publishedAt"] == nil {
		t.Error("publishedAt should be set")
	}
}

func TestWriter_PutShare_NilPublishedAt(t *testing.T) {
	pdsURL, cap := writerMock(t)
	w := NewWriter(atclient.NewAPIClient(pdsURL), "did:plc:alice")
	_, err := w.PutShare(context.Background(), "https://x.com", "Title", "", "", "", "", "", nil, time.Now())
	if err != nil {
		t.Fatalf("PutShare nil publishedAt: %v", err)
	}
	if v, ok := cap.Value["publishedAt"]; ok && v != "" {
		t.Errorf("publishedAt should be absent/empty when nil, got %v", v)
	}
}

func TestWriter_PutFeedSubscription(t *testing.T) {
	pdsURL, cap := writerMock(t)
	w := NewWriter(atclient.NewAPIClient(pdsURL), "did:plc:alice")
	rkey, err := w.PutFeedSubscription(context.Background(),
		"https://example.com/feed.xml", "https://example.com", "Example Feed", time.Now(),
	)
	if err != nil {
		t.Fatalf("PutFeedSubscription: %v", err)
	}
	if len(rkey) != 13 {
		t.Errorf("rkey length=%d, want 13", len(rkey))
	}
	if cap.Collection != CollectionSubscription {
		t.Errorf("collection=%q, want %q", cap.Collection, CollectionSubscription)
	}
	if cap.Value["feedUrl"] != "https://example.com/feed.xml" {
		t.Errorf("feedUrl=%v", cap.Value["feedUrl"])
	}
}

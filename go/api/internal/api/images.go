package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// imageCacheTTL is how long a fetched blob stays in the in-memory cache before
// being re-fetched from the PDS. Blobs are content-addressed by CID in the
// getBlob URL, so the bytes never change for a given URL — a re-fetch only
// happens after a process restart or TTL expiry and returns identical bytes.
const imageCacheTTL = 24 * time.Hour

// cachedImage holds the bytes, content-type, and fetch time of a proxied
// image. The cache key is the PDS getBlob URL.
type cachedImage struct {
	bytes       []byte
	contentType string
	fetchedAt   time.Time
}

// imageCache is a process-wide in-memory cache for proxied profile images.
// It deduplicates concurrent fetches for the same URL (so a burst of avatar
// requests for the same user hits the PDS once) and serves cached bytes with
// long Cache-Control so the browser caches too. Entries expire after
// imageCacheTTL; eviction is lazy on read.
type imageCache struct {
	mu     sync.Mutex
	items  map[string]cachedImage
	pending map[string]*singleflight // inflight dedup
}

type singleflight struct {
	done chan struct{}
	res  cachedImage
	err  error
}

func newImageCache() *imageCache {
	return &imageCache{
		items:   make(map[string]cachedImage),
		pending: make(map[string]*singleflight),
	}
}

// get returns the cached image for url if fresh, or fetches it via the provided
// client. Concurrent calls for the same URL block on a single fetch.
func (c *imageCache) get(ctx context.Context, url string, fetch func(context.Context, string) (cachedImage, error)) (cachedImage, error) {
	c.mu.Lock()
	if it, ok := c.items[url]; ok && time.Since(it.fetchedAt) < imageCacheTTL {
		c.mu.Unlock()
		return it, nil
	}
	// Deduplicate: if a fetch is already in flight, wait on it.
	if sf, ok := c.pending[url]; ok {
		c.mu.Unlock()
		select {
		case <-sf.done:
			return sf.res, sf.err
		case <-ctx.Done():
			return cachedImage{}, ctx.Err()
		}
	}
	sf := &singleflight{done: make(chan struct{})}
	c.pending[url] = sf
	c.mu.Unlock()

	res, err := fetch(ctx, url)

	c.mu.Lock()
	delete(c.pending, url)
	if err == nil {
		c.items[url] = res
	}
	c.mu.Unlock()

	sf.res = res
	sf.err = err
	close(sf.done)
	return res, err
}

// imageClient is the HTTP client used to fetch blobs from a PDS. Lazily built
// on first use so --openapi generation (nil fetcher) doesn't need it.
var (
	imageClientOnce sync.Once
	imageClient     *http.Client
)

func getImageClient() *http.Client {
	imageClientOnce.Do(func() {
		imageClient = &http.Client{Timeout: 15 * time.Second}
	})
	return imageClient
}

// fetchImage fetches the blob at url and returns its bytes + content-type.
func (a *API) fetchImage(ctx context.Context, url string) (cachedImage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return cachedImage{}, err
	}
	req.Header.Set("User-Agent", "Sunred")
	resp, err := getImageClient().Do(req)
	if err != nil {
		return cachedImage{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return cachedImage{}, errors.New("upstream status " + resp.Status)
	}
	// Cap at 5 MiB — avatars/banners are well under that; anything larger is
	// almost certainly not an image and not worth caching in memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return cachedImage{}, err
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	return cachedImage{bytes: body, contentType: ct, fetchedAt: time.Now()}, nil
}

// imageCacheInstance is the package-level cache shared across requests.
var imageCacheInstance = newImageCache()

// serveImage looks up the PDS URL for the user's avatar/banner and streams the
// cached bytes back with long-lived cache headers. Returns 404 when the user
// has no image set (so the client can fall back to initials).
func (a *API) serveImage(ctx context.Context, handle string, kind string) (*huma.StreamResponse, error) {
	avatar, banner, err := a.store.GetAvatarBannerByHandle(ctx, handle)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	src := avatar
	if kind == "banner" {
		src = banner
	}
	if src == "" {
		return nil, huma.Error404NotFound("no image")
	}

	img, err := imageCacheInstance.get(ctx, src, a.fetchImage)
	if err != nil {
		slog.Warn("image proxy: fetch", "handle", handle, "kind", kind, "url", src, "err", err)
		return nil, huma.Error502BadGateway("could not fetch image")
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", img.contentType)
			ctx.SetHeader("Cache-Control", "public, max-age=86400, immutable")
			w := ctx.BodyWriter()
			_, _ = w.Write(img.bytes)
		},
	}, nil
}

// --- Routes ---

type ImageInput struct {
	Handle string `path:"handle"`
}

func (a *API) registerImageRoutes() {
	huma.Register(a.huma, huma.Operation{
		OperationID: "get-user-avatar",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/{handle}/avatar",
		Summary:     "Get a user's avatar image (proxied from their PDS)",
		Tags:        []string{"users"},
	}, func(ctx context.Context, input *ImageInput) (*huma.StreamResponse, error) {
		return a.serveImage(ctx, input.Handle, "avatar")
	})

	huma.Register(a.huma, huma.Operation{
		OperationID: "get-user-banner",
		Method:      http.MethodGet,
		Path:        "/api/v1/users/{handle}/banner",
		Summary:     "Get a user's banner image (proxied from their PDS)",
		Tags:        []string{"users"},
	}, func(ctx context.Context, input *ImageInput) (*huma.StreamResponse, error) {
		return a.serveImage(ctx, input.Handle, "banner")
	})
}

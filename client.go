package webdavds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errNotFound is the internal sentinel for a 404; the Datastore maps it to
// datastore.ErrNotFound.
var errNotFound = errors.New("webdavds: not found")

// client is a thin WebDAV client over net/http. It owns the tuned transport,
// a concurrency semaphore, URL/path construction, and bounded retries.
type client struct {
	cfg      Config
	urlBase  string // cfg.URL with no trailing slash, no RootDirectory
	root     string // cfg.RootDirectory (no surrounding slashes)
	rootPath string // URL path of urlBase/root, e.g. "/remote.php/dav/files/ipfs/kubo" ("" if none)
	hc       *http.Client
	sem      chan struct{}
}

func newClient(cfg Config) (*client, error) {
	if err := cfg.normalize(); err != nil {
		return nil, err
	}
	full := cfg.URL
	if cfg.RootDirectory != "" {
		full = cfg.URL + "/" + cfg.RootDirectory
	}
	u, err := url.Parse(full)
	if err != nil {
		return nil, fmt.Errorf("webdavds: invalid url %q: %w", full, err)
	}
	rootPath := ""
	if p := strings.Trim(u.Path, "/"); p != "" {
		rootPath = "/" + p
	}
	tr := &http.Transport{
		ForceAttemptHTTP2:     true,
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: cfg.ConnTimeout, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   256,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   cfg.ConnTimeout,
		ExpectContinueTimeout: time.Second,
		ResponseHeaderTimeout: cfg.RequestTimeout,
		WriteBufferSize:       64 << 10,
		ReadBufferSize:        64 << 10,
	}
	return &client{
		cfg:      cfg,
		urlBase:  cfg.URL,
		root:     cfg.RootDirectory,
		rootPath: rootPath,
		hc:       &http.Client{Transport: tr},
		sem:      make(chan struct{}, cfg.Concurrency),
	}, nil
}

// fullPath prefixes the RootDirectory onto a key-relative path.
func (c *client) fullPath(p string) string {
	p = strings.TrimPrefix(p, "/")
	if c.root == "" {
		return p
	}
	if p == "" {
		return c.root
	}
	return c.root + "/" + p
}

func escapePath(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

// fullURL builds an absolute URL for an already-root-prefixed path.
func (c *client) fullURL(fullpath string) string {
	if fullpath == "" {
		return c.urlBase + "/"
	}
	return c.urlBase + "/" + escapePath(fullpath)
}

// relPath strips the rootPath prefix off a server-returned href path, yielding
// a key-relative path (no leading slash), e.g. "blocks/za/CIQ...".
func (c *client) relPath(hrefPath string) string {
	p := "/" + strings.Trim(hrefPath, "/")
	if c.rootPath != "" {
		p = strings.TrimPrefix(p, c.rootPath)
	}
	return strings.Trim(p, "/")
}

// cancelBody fires the per-request context cancel when the body is closed.
type cancelBody struct {
	rc     io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Read(p []byte) (int, error) { return b.rc.Read(p) }
func (b *cancelBody) Close() error {
	err := b.rc.Close()
	b.cancel()
	return err
}

// doAbs performs a single request against an already-root-prefixed path.
// The caller MUST close resp.Body.
func (c *client) doAbs(ctx context.Context, method, fullpath string, body []byte, hdr map[string]string) (*http.Response, error) {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	rctx, cancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(rctx, method, c.fullURL(fullpath), rdr)
	if err != nil {
		cancel()
		<-c.sem
		return nil, err
	}
	if c.cfg.Username != "" {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		cancel()
		<-c.sem
		return nil, err
	}
	sem := c.sem
	resp.Body = &cancelBody{rc: resp.Body, cancel: func() { cancel(); <-sem }}
	return resp, nil
}

// do performs a request against a key-relative path.
func (c *client) do(ctx context.Context, method, p string, body []byte, hdr map[string]string) (*http.Response, error) {
	return c.doAbs(ctx, method, c.fullPath(p), body, hdr)
}

func drain(resp *http.Response) {
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func (c *client) put(ctx context.Context, p string, val []byte) error {
	return c.withRetry(ctx, func() error {
		resp, err := c.do(ctx, http.MethodPut, p, val, nil)
		if err != nil {
			return err
		}
		defer drain(resp)
		if resp.StatusCode/100 == 2 {
			return nil
		}
		return statusErr("PUT", p, resp)
	})
}

func (c *client) get(ctx context.Context, p string) ([]byte, error) {
	var out []byte
	err := c.withRetry(ctx, func() error {
		resp, err := c.do(ctx, http.MethodGet, p, nil, nil)
		if err != nil {
			return err
		}
		defer drain(resp)
		if resp.StatusCode == http.StatusNotFound {
			return errNotFound
		}
		if resp.StatusCode/100 != 2 {
			return statusErr("GET", p, resp)
		}
		out, err = io.ReadAll(resp.Body)
		return err
	})
	return out, err
}

// size returns the content length of a single resource via PROPFIND Depth:0.
func (c *client) size(ctx context.Context, p string) (int64, error) {
	entries, err := c.propfind(ctx, p, 0)
	if err != nil {
		return -1, err
	}
	if len(entries) == 0 {
		return -1, errNotFound
	}
	return entries[0].Size, nil
}

func (c *client) delete(ctx context.Context, p string) error {
	resp, err := c.do(ctx, http.MethodDelete, p, nil, nil)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode == http.StatusNotFound {
		return nil // idempotent
	}
	if resp.StatusCode/100 == 2 {
		return nil
	}
	return statusErr("DELETE", p, resp)
}

func (c *client) move(ctx context.Context, from, to string) error {
	return c.withRetry(ctx, func() error {
		resp, err := c.do(ctx, "MOVE", from, nil, map[string]string{
			"Destination": c.fullURL(c.fullPath(to)),
			"Overwrite":   "T",
		})
		if err != nil {
			return err
		}
		defer drain(resp)
		if resp.StatusCode/100 == 2 {
			return nil
		}
		return statusErr("MOVE", from, resp)
	})
}

// mkcolRaw creates a single collection at an already-root-prefixed path.
func (c *client) mkcolRaw(ctx context.Context, fullpath string) error {
	resp, err := c.doAbs(ctx, "MKCOL", fullpath, nil, nil)
	if err != nil {
		return err
	}
	defer drain(resp)
	switch resp.StatusCode {
	case http.StatusMethodNotAllowed: // already exists
		return nil
	}
	if resp.StatusCode/100 == 2 {
		return nil
	}
	return statusErr("MKCOL", fullpath, resp)
}

// mkcolAll creates a key-relative collection and all its ancestors (including
// the RootDirectory), idempotently.
func (c *client) mkcolAll(ctx context.Context, p string) error {
	full := c.fullPath(p)
	parts := strings.Split(strings.Trim(full, "/"), "/")
	cur := ""
	for _, seg := range parts {
		if seg == "" {
			continue
		}
		if cur == "" {
			cur = seg
		} else {
			cur += "/" + seg
		}
		if err := c.mkcolRaw(ctx, cur); err != nil {
			return err
		}
	}
	return nil
}

// options probes the server; returns the Allow and DAV headers.
func (c *client) options(ctx context.Context) (allow, dav string, err error) {
	resp, err := c.do(ctx, http.MethodOptions, "", nil, nil)
	if err != nil {
		return "", "", err
	}
	defer drain(resp)
	if resp.StatusCode/100 != 2 {
		return "", "", statusErr("OPTIONS", "", resp)
	}
	return resp.Header.Get("Allow"), resp.Header.Get("DAV"), nil
}

func (c *client) close() { c.hc.CloseIdleConnections() }

func statusErr(method, p string, resp *http.Response) error {
	return &statusError{method: method, path: p, code: resp.StatusCode, status: resp.Status}
}

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// withRetry retries idempotent operations on transient errors with backoff.
func (c *client) withRetry(ctx context.Context, fn func() error) error {
	const maxAttempts = 4
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = fn()
		if err == nil || errors.Is(err, errNotFound) {
			return err
		}
		// Retry network errors and a few transient HTTP statuses.
		var retry bool
		var se *statusError
		if errors.As(err, &se) {
			retry = isRetryableStatus(se.code)
		} else {
			retry = true // network/transport error
		}
		if !retry || attempt == maxAttempts-1 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * 100 * time.Millisecond):
		}
	}
	return err
}

// statusError carries the HTTP status code for retry decisions.
type statusError struct {
	method string
	path   string
	code   int
	status string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("webdavds: %s %q: %s", e.method, e.path, e.status)
}

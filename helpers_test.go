package webdavds

import (
	"net/http/httptest"
	"testing"

	"golang.org/x/net/webdav"
)

// newWebDAVServer starts an in-process WebDAV server backed by an in-memory FS.
func newWebDAVServer(t *testing.T) *httptest.Server {
	t.Helper()
	h := &webdav.Handler{
		FileSystem: webdav.NewMemFS(),
		LockSystem: webdav.NewMemLS(),
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// newTestClient returns a bare client (no datastore skeleton/probe).
func newTestClient(t *testing.T) *client {
	t.Helper()
	srv := newWebDAVServer(t)
	c, err := newClient(Config{URL: srv.URL, Concurrency: 4})
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

// newTestDatastore returns a fully started datastore against a fresh server.
func newTestDatastore(t *testing.T) *Datastore {
	t.Helper()
	srv := newWebDAVServer(t)
	d, err := New(Config{URL: srv.URL, Concurrency: 4})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

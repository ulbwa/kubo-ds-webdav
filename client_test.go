package webdavds

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"golang.org/x/net/webdav"
)

// TestMkcolRetriesLocked simulates a DAV class-2 server that momentarily locks
// the parent collection during concurrent shard-directory creation, returning
// 423 Locked to a sibling MKCOL. The client must retry rather than abort the
// whole batch (which is what breaks `ipfs add` of large files at concurrency>1).
func TestMkcolRetriesLocked(t *testing.T) {
	dav := &webdav.Handler{
		FileSystem: webdav.NewMemFS(),
		LockSystem: webdav.NewMemLS(),
	}
	var mkcols int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "MKCOL" && atomic.AddInt32(&mkcols, 1) == 1 {
			http.Error(w, "Locked", http.StatusLocked)
			return
		}
		dav.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	c, err := newClient(Config{URL: srv.URL, Concurrency: 4})
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	if err := c.mkcolRaw(context.Background(), "blocks"); err != nil {
		t.Fatalf("mkcolRaw must retry 423 Locked, got: %v", err)
	}
	if got := atomic.LoadInt32(&mkcols); got < 2 {
		t.Fatalf("expected a retry after 423, only %d MKCOL(s) seen", got)
	}
}

func TestClientPutGetSizeDelete(t *testing.T) {
	cl := newTestClient(t)
	ctx := context.Background()
	if err := cl.mkcolAll(ctx, "a/b"); err != nil {
		t.Fatal(err)
	}
	if err := cl.put(ctx, "a/b/k.data", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := cl.get(ctx, "a/b/k.data")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("got %q", got)
	}
	sz, err := cl.size(ctx, "a/b/k.data")
	if err != nil || sz != 5 {
		t.Fatalf("size=%d err=%v", sz, err)
	}
	if err := cl.delete(ctx, "a/b/k.data"); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.get(ctx, "a/b/k.data"); !errors.Is(err, errNotFound) {
		t.Fatalf("want errNotFound got %v", err)
	}
	if err := cl.delete(ctx, "a/b/k.data"); err != nil {
		t.Fatalf("delete must be idempotent: %v", err)
	}
}

func TestClientMove(t *testing.T) {
	cl := newTestClient(t)
	ctx := context.Background()
	cl.mkcolAll(ctx, "d")
	if err := cl.put(ctx, "d/.tmp-1", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if err := cl.move(ctx, "d/.tmp-1", "d/final"); err != nil {
		t.Fatal(err)
	}
	if _, err := cl.get(ctx, "d/.tmp-1"); !errors.Is(err, errNotFound) {
		t.Fatalf("source should be gone: %v", err)
	}
	v, err := cl.get(ctx, "d/final")
	if err != nil || string(v) != "v" {
		t.Fatalf("dest wrong: %q %v", v, err)
	}
}

func TestClientPropfind(t *testing.T) {
	cl := newTestClient(t)
	ctx := context.Background()
	cl.mkcolAll(ctx, "blocks/za")
	cl.put(ctx, "blocks/za/k1", []byte("aa"))
	cl.put(ctx, "blocks/za/k2", []byte("bbb"))
	entries, err := cl.propfind(ctx, "blocks/za", 1)
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, e := range entries {
		if !e.IsDir {
			files++
		}
	}
	if files != 2 {
		t.Fatalf("want 2 files got %d (%+v)", files, entries)
	}
}

func TestClientOptions(t *testing.T) {
	cl := newTestClient(t)
	cl.mkcolAll(context.Background(), "")
	allow, _, err := cl.options(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if allow == "" {
		t.Fatal("expected non-empty Allow header")
	}
}

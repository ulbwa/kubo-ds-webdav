package webdavds

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

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

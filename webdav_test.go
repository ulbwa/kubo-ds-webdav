package webdavds

import (
	"context"
	"errors"
	"sort"
	"testing"

	ds "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"
)

func TestDatastoreRoundTripBlock(t *testing.T) {
	d := newTestDatastore(t)
	ctx := context.Background()
	k := ds.NewKey("/blocks/CIQEXAMPLEKEY1234567890")
	if err := d.Put(ctx, k, []byte("blockdata")); err != nil {
		t.Fatal(err)
	}
	ok, err := d.Has(ctx, k)
	if err != nil || !ok {
		t.Fatalf("has=%v err=%v", ok, err)
	}
	v, err := d.Get(ctx, k)
	if err != nil || string(v) != "blockdata" {
		t.Fatalf("get=%q err=%v", v, err)
	}
	sz, err := d.GetSize(ctx, k)
	if err != nil || sz != 9 {
		t.Fatalf("size=%d err=%v", sz, err)
	}
	if err := d.Delete(ctx, k); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Get(ctx, k); !errors.Is(err, ds.ErrNotFound) {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}

func TestDatastoreMetadataKey(t *testing.T) {
	d := newTestDatastore(t)
	ctx := context.Background()
	k := ds.NewKey("/local/filesroot")
	if err := d.Put(ctx, k, []byte("Qmroot")); err != nil {
		t.Fatal(err)
	}
	v, err := d.Get(ctx, k)
	if err != nil || string(v) != "Qmroot" {
		t.Fatalf("get=%q err=%v", v, err)
	}
}

func TestDatastoreNotFound(t *testing.T) {
	d := newTestDatastore(t)
	ctx := context.Background()
	if _, err := d.Get(ctx, ds.NewKey("/blocks/MISSING")); !errors.Is(err, ds.ErrNotFound) {
		t.Fatalf("want ErrNotFound got %v", err)
	}
	if ok, err := d.Has(ctx, ds.NewKey("/blocks/MISSING")); err != nil || ok {
		t.Fatalf("has=%v err=%v", ok, err)
	}
	if _, err := d.GetSize(ctx, ds.NewKey("/blocks/MISSING")); !errors.Is(err, ds.ErrNotFound) {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}

func TestDatastoreShardingPersisted(t *testing.T) {
	srv := newWebDAVServer(t)
	d1, err := New(Config{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	d1.Close()
	// A second open with an explicit, conflicting shard func must fail.
	if _, err := New(Config{URL: srv.URL, ShardFunc: "/repo/flatfs/shard/v1/prefix/2"}); err == nil {
		t.Fatal("expected shard mismatch error")
	}
	// A second open with no shard func adopts the stored one and succeeds.
	d2, err := New(Config{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	d2.Close()
}

func TestDatastoreNoDelete(t *testing.T) {
	srv := newWebDAVServer(t)
	d, err := New(Config{URL: srv.URL, NoDelete: true})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Delete(context.Background(), ds.NewKey("/blocks/X")); err == nil {
		t.Fatal("expected delete to be rejected")
	}
}

func TestDatastoreQuery(t *testing.T) {
	d := newTestDatastore(t)
	ctx := context.Background()
	keys := []string{"/blocks/CIQAAA111", "/blocks/CIQBBB222", "/blocks/CIQCCC333"}
	for _, k := range keys {
		if err := d.Put(ctx, ds.NewKey(k), []byte(k)); err != nil {
			t.Fatal(err)
		}
	}
	// also a metadata key that must NOT appear under the /blocks prefix
	d.Put(ctx, ds.NewKey("/local/x"), []byte("meta"))

	res, err := d.Query(ctx, dsq.Query{Prefix: "/blocks", KeysOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	all, err := res.Rest()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range all {
		got = append(got, e.Key)
	}
	sort.Strings(got)
	if len(got) != 3 {
		t.Fatalf("want 3 block keys got %d: %v", len(got), got)
	}
	for i, k := range keys {
		if got[i] != k {
			t.Fatalf("key %d: got %q want %q", i, got[i], k)
		}
	}

	// Query with values + limit.
	res2, err := d.Query(ctx, dsq.Query{Prefix: "/blocks", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	all2, _ := res2.Rest()
	if len(all2) != 2 {
		t.Fatalf("limit not applied: got %d", len(all2))
	}
	for _, e := range all2 {
		if len(e.Value) == 0 {
			t.Fatalf("expected value for key %q", e.Key)
		}
	}
}

func TestDatastoreBatch(t *testing.T) {
	d := newTestDatastore(t)
	ctx := context.Background()
	b, err := d.Batch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"/blocks/CIQB1", "/blocks/CIQB2", "/blocks/CIQB3"} {
		if err := b.Put(ctx, ds.NewKey(k), []byte("v")); err != nil {
			t.Fatal(err)
		}
	}
	b.Delete(ctx, ds.NewKey("/blocks/CIQB2"))
	if err := b.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, _ := d.Has(ctx, ds.NewKey("/blocks/CIQB1")); !ok {
		t.Fatal("CIQB1 should exist")
	}
	if ok, _ := d.Has(ctx, ds.NewKey("/blocks/CIQB2")); ok {
		t.Fatal("CIQB2 should be deleted")
	}
}

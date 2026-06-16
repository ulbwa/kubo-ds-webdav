//go:build integration

package webdavds

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	dstest "github.com/ipfs/go-datastore/test"
)

// webdavTestURL is the base URL of a real WebDAV server (Apache mod_dav).
func webdavTestURL() string {
	if u := os.Getenv("WEBDAV_TEST_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:8091"
}

// TestSuite runs the full go-datastore conformance suite against a real
// WebDAV backend. Each run uses a unique root directory and cleans it up.
func TestSuite(t *testing.T) {
	// The conformance suite's SubtestCombinations runs ~1152 query
	// permutations, re-seeding data each time. Against a network WebDAV that is
	// very chatty; a smaller element count keeps it fast while still exercising
	// offset/limit/filter/order/prefix logic (this is the same value the suite
	// itself uses under the race detector).
	dstest.ElemCount = 20
	root := fmt.Sprintf("suite-%d", time.Now().UnixNano())
	// The minimal Apache test image serializes writes through a gdbm lock DB;
	// a moderate concurrency keeps contention (and retries) reasonable. Real
	// WebDAV servers handle far more.
	d, err := New(Config{URL: webdavTestURL(), RootDirectory: root, Concurrency: 8})
	if err != nil {
		t.Fatalf("New against %s: %v", webdavTestURL(), err)
	}
	t.Cleanup(func() {
		// Recursively remove the test root (Apache MOVE/DELETE on a collection
		// is recursive).
		_ = d.c.delete(context.Background(), "")
		d.Close()
	})
	dstest.SubtestAll(t, d)
}

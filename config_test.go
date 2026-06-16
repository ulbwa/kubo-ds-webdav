package webdavds

import (
	"testing"
	"time"
)

func TestConfigDefaultsAndEnv(t *testing.T) {
	t.Setenv("WD_PASS", "secret")
	c := Config{URL: "http://h/dav/", Password: "${WD_PASS}", Headers: map[string]string{"X": "${WD_PASS}"}}
	if err := c.normalize(); err != nil {
		t.Fatal(err)
	}
	if c.Password != "secret" {
		t.Fatalf("password env not expanded: %q", c.Password)
	}
	if c.Headers["X"] != "secret" {
		t.Fatalf("header env not expanded: %q", c.Headers["X"])
	}
	if c.URL != "http://h/dav" {
		t.Fatalf("trailing slash not trimmed: %q", c.URL)
	}
	if c.Concurrency != 32 {
		t.Fatalf("default concurrency wrong: %d", c.Concurrency)
	}
	if c.RequestTimeout != 60*time.Second {
		t.Fatalf("default request timeout wrong: %v", c.RequestTimeout)
	}
	if c.ShardFunc != DefaultShard {
		t.Fatalf("default shard wrong: %q", c.ShardFunc)
	}
}

func TestConfigRequiresURL(t *testing.T) {
	c := Config{}
	if err := c.normalize(); err == nil {
		t.Fatal("expected error for missing url")
	}
}

func TestConfigRejectsBadShard(t *testing.T) {
	c := Config{URL: "http://h", ShardFunc: "nonsense"}
	if err := c.normalize(); err == nil {
		t.Fatal("expected error for bad shard func")
	}
}

package plugin

import "testing"

func TestParserRequiresURL(t *testing.T) {
	p := WebDAVPlugin{}
	parse := p.DatastoreConfigParser()
	if _, err := parse(map[string]interface{}{}); err == nil {
		t.Fatal("expected error when url missing")
	}
}

func TestParserBuildsConfig(t *testing.T) {
	p := WebDAVPlugin{}
	parse := p.DatastoreConfigParser()
	cfg, err := parse(map[string]interface{}{
		"url":           "https://dav.example.com/dav",
		"rootDirectory": "kubo",
		"username":      "u",
		"password":      "p",
		"concurrency":   float64(16), // JSON numbers arrive as float64
		"noDelete":      true,
		"headers":       map[string]interface{}{"X-Tok": "abc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := cfg.DiskSpec()
	if spec["url"] != "https://dav.example.com/dav" {
		t.Fatalf("disk spec url wrong: %v", spec["url"])
	}
	if spec["rootDirectory"] != "kubo" {
		t.Fatalf("disk spec rootDirectory wrong: %v", spec["rootDirectory"])
	}
	// Credentials must NOT appear in the disk spec fingerprint.
	if _, ok := spec["password"]; ok {
		t.Fatal("password must not be in DiskSpec")
	}
	if _, ok := spec["username"]; ok {
		t.Fatal("username must not be in DiskSpec")
	}
}

func TestParserRejectsBadConcurrency(t *testing.T) {
	p := WebDAVPlugin{}
	parse := p.DatastoreConfigParser()
	if _, err := parse(map[string]interface{}{"url": "http://h", "concurrency": float64(-1)}); err == nil {
		t.Fatal("expected error for negative concurrency")
	}
}

package webdavds

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config configures a WebDAV-backed datastore.
//
// Only URL is required. Credentials and header values support ${ENV_VAR}
// expansion so secrets never have to live in the committed kubo config.
type Config struct {
	// URL is the WebDAV base endpoint, e.g.
	// "https://dav.example.com/remote.php/dav/files/ipfs". Required.
	URL string

	// RootDirectory is an optional sub-path (collection) under URL that this
	// datastore owns. Multiple kubo instances pointed at the same URL +
	// RootDirectory share the same blocks and pins.
	RootDirectory string

	// Username / Password enable HTTP Basic auth. ${ENV_VAR} expanded.
	Username string
	Password string

	// Headers are extra request headers (e.g. a bearer token). Values are
	// ${ENV_VAR} expanded.
	Headers map[string]string

	// ShardFunc is a flatfs shard specification used for block keys.
	// Defaults to DefaultShard. Immutable once written to the SHARDING file.
	ShardFunc string

	// Concurrency bounds the number of in-flight WebDAV requests. Default 32.
	Concurrency int

	// ConnTimeout bounds dial + TLS handshake. Default 10s.
	ConnTimeout time.Duration

	// RequestTimeout bounds a single request/response. Default 60s.
	RequestTimeout time.Duration

	// UseMove selects the write strategy: nil = auto-probe (PUT-temp+MOVE when
	// the server advertises MOVE, otherwise direct PUT); non-nil forces it.
	UseMove *bool

	// NoDelete makes Delete return an error. Set this on follower nodes so only
	// a single coordinator node can run GC against the shared backend.
	NoDelete bool
}

func expandEnv(s string) string {
	if !strings.Contains(s, "$") {
		return s
	}
	return os.Expand(s, os.Getenv)
}

// normalize fills in defaults, expands env vars, and validates the config.
func (c *Config) normalize() error {
	c.URL = strings.TrimRight(strings.TrimSpace(c.URL), "/")
	if c.URL == "" {
		return fmt.Errorf("webdavds: url is required")
	}
	c.RootDirectory = strings.Trim(c.RootDirectory, "/")
	c.Username = expandEnv(c.Username)
	c.Password = expandEnv(c.Password)
	if c.Headers != nil {
		for k, v := range c.Headers {
			c.Headers[k] = expandEnv(v)
		}
	}
	if c.ShardFunc == "" {
		c.ShardFunc = DefaultShard
	}
	if _, err := ParseShardFunc(c.ShardFunc); err != nil {
		return err
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 32
	}
	if c.ConnTimeout <= 0 {
		c.ConnTimeout = 10 * time.Second
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 60 * time.Second
	}
	return nil
}

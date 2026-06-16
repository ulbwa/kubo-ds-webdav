package webdavds

import (
	"fmt"
	"strconv"
	"strings"
)

// Shard functions are wire-compatible with go-ds-flatfs so that the on-disk
// (on-WebDAV) layout, and the SHARDING marker file, match what IPFS operators
// already understand. See:
// https://github.com/ipfs/go-ds-flatfs/blob/master/shard.go

const (
	shardV1Prefix = "/repo/flatfs/shard/v1"

	// DefaultShard is flatfs' default: two characters immediately before the
	// last character of the (base32) key. It spreads block files across 32^2 =
	// 1024 directories, which keeps any single WebDAV collection small enough
	// to PROPFIND quickly even with millions of blocks.
	DefaultShard = "/repo/flatfs/shard/v1/next-to-last/2"
)

// ShardFunc maps a (slash-stripped) key to the name of the directory the value
// should live in.
type ShardFunc struct {
	str string
	fun func(string) string
}

// String returns the canonical flatfs shard specification string.
func (s *ShardFunc) String() string { return s.str }

// Func returns the shard directory name for the given key (without leading slash).
func (s *ShardFunc) Func(noslash string) string { return s.fun(noslash) }

// ParseShardFunc parses a flatfs shard specification string.
func ParseShardFunc(str string) (*ShardFunc, error) {
	str = strings.TrimSpace(str)
	if str == "" {
		return nil, fmt.Errorf("webdavds: empty shard function")
	}
	if !strings.HasPrefix(str, shardV1Prefix+"/") {
		return nil, fmt.Errorf("webdavds: invalid shard function %q", str)
	}
	rest := strings.TrimPrefix(str, shardV1Prefix+"/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("webdavds: invalid shard function %q", str)
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n <= 0 {
		return nil, fmt.Errorf("webdavds: invalid shard parameter in %q", str)
	}
	switch parts[0] {
	case "prefix":
		return &ShardFunc{str, func(k string) string { return padRight(k, n)[:n] }}, nil
	case "suffix":
		return &ShardFunc{str, func(k string) string {
			p := padLeft(k, n)
			return p[len(p)-n:]
		}}, nil
	case "next-to-last":
		return &ShardFunc{str, func(k string) string {
			p := padLeft(k, n+1)
			off := len(p) - n - 1
			return p[off : off+n]
		}}, nil
	default:
		return nil, fmt.Errorf("webdavds: unknown shard function %q", parts[0])
	}
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += "_"
	}
	return s
}

func padLeft(s string, n int) string {
	for len(s) < n {
		s = "_" + s
	}
	return s
}

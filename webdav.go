// Package webdavds implements a kubo/IPFS datastore (go-datastore Datastore +
// Batching) backed by a remote WebDAV server.
//
// Content-addressed blocks live under "/blocks", sharded flatfs-style so that
// no single WebDAV collection holds millions of files. All other keys (pins,
// MFS root, provider records, ...) are stored at their natural path. Because
// blocks are content-addressed they are idempotent and safe to write
// concurrently, which lets several kubo instances share one WebDAV backend.
package webdavds

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ds "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"
	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("webdavds")

const (
	blocksPrefix = "/blocks/"
	shardingKey  = "SHARDING"

	// dataSuffix is appended to every stored value's filename (like flatfs'
	// ".data"). It guarantees a leaf file and a directory never share a name,
	// so keys where one is a path-prefix of another (e.g. "/a" and "/a/b") can
	// coexist on a real WebDAV hierarchy — which, unlike S3, has real
	// collections.
	dataSuffix = ".data"
)

// Datastore is a WebDAV-backed go-datastore.
type Datastore struct {
	c          *client
	shard      *ShardFunc
	cache      *lru
	noDelete   bool
	useMove    bool
	tmpCounter uint64

	// dirs memoizes collections known to exist (full, root-prefixed paths) so
	// we MKCOL each shard directory at most once per process instead of on
	// every Put — essential when writing millions of blocks.
	dirs sync.Map
}

// Compile-time interface checks.
var (
	_ ds.Datastore = (*Datastore)(nil)
	_ ds.Batching  = (*Datastore)(nil)
)

// New connects to the WebDAV backend, probes its capabilities, creates the
// directory skeleton, and reconciles the SHARDING marker.
func New(cfg Config) (*Datastore, error) {
	explicitShard := strings.TrimSpace(cfg.ShardFunc) != ""
	c, err := newClient(cfg)
	if err != nil {
		return nil, err
	}
	sf, err := ParseShardFunc(c.cfg.ShardFunc)
	if err != nil {
		c.close()
		return nil, err
	}
	d := &Datastore{
		c:        c,
		shard:    sf,
		cache:    newLRU(1 << 16),
		noDelete: c.cfg.NoDelete,
	}
	if err := d.start(context.Background(), explicitShard); err != nil {
		c.close()
		return nil, err
	}
	return d, nil
}

func (d *Datastore) start(ctx context.Context, explicitShard bool) error {
	// Create the skeleton first; this also validates connectivity + auth and
	// gives the capability probe an existing collection to inspect.
	if err := d.ensureDir(ctx, "blocks"); err != nil {
		return fmt.Errorf("webdavds: cannot create skeleton at %s: %w", d.c.urlBase, err)
	}

	// Capability probe: decide the write strategy for mutable (non-block) keys.
	allow, dav, err := d.c.options(ctx)
	if err != nil {
		return fmt.Errorf("webdavds: capability probe failed for %s: %w", d.c.urlBase, err)
	}
	if d.c.cfg.UseMove != nil {
		d.useMove = *d.c.cfg.UseMove
	} else {
		// A server advertising any DAV compliance class supports MOVE
		// (mandatory in RFC 4918 class 1), even if OPTIONS' Allow omits it.
		d.useMove = strings.Contains(strings.ToUpper(allow), "MOVE") || strings.TrimSpace(dav) != ""
	}
	log.Infow("webdav datastore ready", "url", d.c.urlBase, "root", d.c.root,
		"useMove", d.useMove, "dav", dav, "shard", d.shard.String())

	return d.ensureSharding(ctx, explicitShard)
}

// ensureSharding writes the SHARDING marker on a fresh store, or reconciles it
// against an existing one.
func (d *Datastore) ensureSharding(ctx context.Context, explicitShard bool) error {
	data, err := d.c.get(ctx, shardingKey)
	if errors.Is(err, errNotFound) {
		return d.c.put(ctx, shardingKey, []byte(d.shard.String()))
	}
	if err != nil {
		return fmt.Errorf("webdavds: reading SHARDING: %w", err)
	}
	stored := strings.TrimSpace(string(data))
	if stored == d.shard.String() {
		return nil
	}
	if explicitShard {
		return fmt.Errorf("webdavds: shard function mismatch: store uses %q but config requests %q", stored, d.shard.String())
	}
	// User didn't override; adopt the existing store's layout.
	sf, perr := ParseShardFunc(stored)
	if perr != nil {
		return fmt.Errorf("webdavds: invalid SHARDING marker %q: %w", stored, perr)
	}
	d.shard = sf
	log.Infow("adopted existing shard function", "shard", stored)
	return nil
}

// pathFor maps a datastore key to (parent dir, full path), both key-relative.
// Block keys are sharded; everything else uses its natural path.
func (d *Datastore) pathFor(k ds.Key) (dir, full string) {
	ks := k.String()
	if strings.HasPrefix(ks, blocksPrefix) {
		name := ks[len(blocksPrefix):]
		dir = "blocks/" + d.shard.Func(name)
		full = dir + "/" + name + dataSuffix
		return dir, full
	}
	rel := strings.TrimPrefix(ks, "/")
	full = rel + dataSuffix
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		dir = rel[:i]
	}
	return dir, full
}

func (d *Datastore) tmpPath(dir string) string {
	n := atomic.AddUint64(&d.tmpCounter, 1)
	name := fmt.Sprintf(".tmp-%d-%d", time.Now().UnixNano(), n)
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// ensureDir creates a key-relative collection and its ancestors, MKCOLing only
// the segments not already known to exist.
func (d *Datastore) ensureDir(ctx context.Context, dir string) error {
	full := d.c.fullPath(dir)
	if full == "" {
		return nil
	}
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
		if _, ok := d.dirs.Load(cur); ok {
			continue
		}
		if err := d.c.mkcolRaw(ctx, cur); err != nil {
			return err
		}
		d.dirs.Store(cur, struct{}{})
	}
	return nil
}

// Put writes a value.
//
// Content-addressed blocks use a direct PUT: they are idempotent, so a direct
// write minimizes round-trips (one per block) and is safe for concurrent
// writers. Mutable (non-block) keys use a temp-PUT+MOVE atomic replace when the
// server supports MOVE, so a torn write never exposes a partial pin/MFS root.
func (d *Datastore) Put(ctx context.Context, k ds.Key, value []byte) error {
	dir, full := d.pathFor(k)
	isBlock := strings.HasPrefix(k.String(), blocksPrefix)

	write := func() error {
		if d.useMove && !isBlock {
			tmp := d.tmpPath(dir)
			if err := d.c.put(ctx, tmp, value); err != nil {
				return err
			}
			if err := d.c.move(ctx, tmp, full); err != nil {
				_ = d.c.delete(ctx, tmp) // best-effort cleanup
				return err
			}
			return nil
		}
		return d.c.put(ctx, full, value)
	}

	if dir != "" {
		if err := d.ensureDir(ctx, dir); err != nil {
			return err
		}
	}
	// A write may 404 if the parent collection isn't visible yet: some WebDAV
	// servers (e.g. rclone's VFS) register a freshly MKCOL'd directory
	// asynchronously. Recreate the parent and retry with backoff. On servers
	// that create directories synchronously this never triggers.
	const maxAttempts = 5
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = write()
		if err == nil || dir == "" || !is404(err) {
			break
		}
		d.invalidateDir(dir)
		if e := d.ensureDir(ctx, dir); e != nil {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * 50 * time.Millisecond):
		}
	}
	if err != nil {
		return err
	}
	d.cache.set(k.String(), len(value))
	return nil
}

// invalidateDir forgets a directory (and its descendants in the cache) so the
// next ensureDir re-MKCOLs it.
func (d *Datastore) invalidateDir(dir string) {
	full := d.c.fullPath(dir)
	d.dirs.Range(func(key, _ any) bool {
		k := key.(string)
		if k == full || strings.HasPrefix(k, full+"/") {
			d.dirs.Delete(key)
		}
		return true
	})
}

func (d *Datastore) Get(ctx context.Context, k ds.Key) ([]byte, error) {
	_, full := d.pathFor(k)
	v, err := d.c.get(ctx, full)
	if errors.Is(err, errNotFound) {
		return nil, ds.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	d.cache.set(k.String(), len(v))
	return v, nil
}

func (d *Datastore) Has(ctx context.Context, k ds.Key) (bool, error) {
	if _, ok := d.cache.get(k.String()); ok {
		return true, nil
	}
	_, full := d.pathFor(k)
	sz, err := d.c.size(ctx, full)
	if errors.Is(err, errNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	d.cache.set(k.String(), int(sz))
	return true, nil
}

func (d *Datastore) GetSize(ctx context.Context, k ds.Key) (int, error) {
	if sz, ok := d.cache.get(k.String()); ok {
		return sz, nil
	}
	_, full := d.pathFor(k)
	sz, err := d.c.size(ctx, full)
	if errors.Is(err, errNotFound) {
		return -1, ds.ErrNotFound
	}
	if err != nil {
		return -1, err
	}
	d.cache.set(k.String(), int(sz))
	return int(sz), nil
}

func (d *Datastore) Delete(ctx context.Context, k ds.Key) error {
	if d.noDelete {
		return fmt.Errorf("webdavds: delete is disabled on this node (noDelete=true)")
	}
	_, full := d.pathFor(k)
	if err := d.c.delete(ctx, full); err != nil {
		return err
	}
	d.cache.del(k.String())
	return nil
}

// Sync is a no-op: WebDAV PUT/MOVE are durable on success.
func (d *Datastore) Sync(ctx context.Context, prefix ds.Key) error { return nil }

func (d *Datastore) Close() error {
	d.c.close()
	return nil
}

// Query walks the WebDAV tree under the query prefix (recursive PROPFIND
// Depth:1, key+size only) and applies filters/orders/offset/limit naively, so
// GC and `ipfs repo` work without the backend supporting rich queries.
//
// Values are fetched lazily: only the entries that survive filtering/limiting
// are GET, unless the query orders or filters by value (in which case every
// value must be materialized up front).
func (d *Datastore) Query(ctx context.Context, q dsq.Query) (dsq.Results, error) {
	// Normalize the prefix the way go-datastore does (e.g. "/a/.." -> "/") so
	// the PROPFIND walk targets a real collection, not a path containing "..".
	// NaiveQueryApply re-cleans the prefix for result filtering on its own.
	walkPrefix := q.Prefix
	if walkPrefix != "" {
		if walkPrefix[0] != '/' {
			walkPrefix = "/" + walkPrefix
		}
		walkPrefix = path.Clean(walkPrefix)
	}
	var entries []dsq.Entry
	if err := d.walk(ctx, strings.Trim(walkPrefix, "/"), &entries); err != nil {
		return nil, err
	}

	needValues := !q.KeysOnly

	if needValues && (hasValueOrder(q.Orders) || hasValueFilter(q.Filters)) {
		// Ordering/filtering by value forces materializing every value first.
		for i := range entries {
			v, err := d.Get(ctx, ds.NewKey(entries[i].Key))
			if errors.Is(err, ds.ErrNotFound) {
				continue // raced with a delete
			}
			if err != nil {
				return nil, err
			}
			entries[i].Value = v
			entries[i].Size = len(v)
		}
		return dsq.NaiveQueryApply(q, dsq.ResultsWithEntries(q, entries)), nil
	}

	// Apply prefix/key-filters/key-orders/offset/limit on keys only...
	keyQuery := q
	keyQuery.KeysOnly = true
	res := dsq.NaiveQueryApply(keyQuery, dsq.ResultsWithEntries(keyQuery, entries))
	if !needValues {
		return res, nil
	}
	// ...then fetch values only for the survivors.
	return dsq.ResultsFromIterator(q, dsq.Iterator{
		Next: func() (dsq.Result, bool) {
			r, ok := res.NextSync()
			if !ok {
				return dsq.Result{}, false
			}
			if r.Error != nil {
				return r, true
			}
			v, err := d.Get(ctx, ds.NewKey(r.Entry.Key))
			if err != nil {
				if errors.Is(err, ds.ErrNotFound) {
					return dsq.Result{}, false
				}
				return dsq.Result{Error: err}, true
			}
			r.Entry.Value = v
			r.Entry.Size = len(v)
			return r, true
		},
		Close: func() error { return res.Close() },
	}), nil
}

// walk recursively collects file entries (key + size, no value) under a
// key-relative directory.
func (d *Datastore) walk(ctx context.Context, dir string, out *[]dsq.Entry) error {
	items, err := d.c.propfind(ctx, dir, 1)
	if errors.Is(err, errNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.IsDir {
			if err := d.walk(ctx, it.Rel, out); err != nil {
				return err
			}
			continue
		}
		key, ok := d.keyForRel(it.Rel)
		if !ok {
			continue
		}
		*out = append(*out, dsq.Entry{Key: key.String(), Size: int(it.Size)})
	}
	return nil
}

func hasValueOrder(orders []dsq.Order) bool {
	for _, o := range orders {
		switch o.(type) {
		case dsq.OrderByValue, dsq.OrderByValueDescending, dsq.OrderByFunction:
			return true
		}
	}
	return false
}

func hasValueFilter(filters []dsq.Filter) bool {
	for _, f := range filters {
		if _, ok := f.(dsq.FilterValueCompare); ok {
			return true
		}
	}
	return false
}

// keyForRel inverts pathFor: a key-relative file path back into a datastore key.
// Only ".data" files are real values; the SHARDING marker and in-flight temp
// files are skipped.
func (d *Datastore) keyForRel(rel string) (ds.Key, bool) {
	rel = strings.Trim(rel, "/")
	if !strings.HasSuffix(rel, dataSuffix) {
		return ds.Key{}, false
	}
	rel = strings.TrimSuffix(rel, dataSuffix)
	if rel == "" {
		return ds.Key{}, false
	}
	if strings.HasPrefix(rel, "blocks/") {
		name := rel[strings.LastIndex(rel, "/")+1:]
		return ds.NewKey(blocksPrefix + name), true
	}
	return ds.NewKey("/" + rel), true
}

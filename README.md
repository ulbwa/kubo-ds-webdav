# kubo-ds-webdav

A [kubo](https://github.com/ipfs/kubo) (Go IPFS) **datastore plugin that stores
blocks and metadata in a WebDAV server**.

The entire datastore — content-addressed blocks **and** pins, MFS root and
provider records — lives in WebDAV. Because content-addressed blocks are
immutable and idempotent, several kubo instances can point at one WebDAV backend
and share the same blocks and pins: more peers, one storage.

Modeled on [`go-ds-s3`](https://github.com/ipfs/go-ds-s3) /
[`kubo-ds-s3`](https://github.com/ulbwa/kubo-ds-s3).

## How it scales to millions of blocks

WebDAV (and the filesystems behind it) is poor at millions of files in one
directory. The plugin handles this the way kubo's own `flatfs` does, plus a few
WebDAV-specific measures:

- **Directory sharding.** Block keys under `/blocks` are spread across `32² =
  1024` collections using the flatfs-compatible `next-to-last/2` scheme (the
  scheme is recorded in a `SHARDING` marker and is configurable at init). At 1M
  blocks that's ~1k files per collection; for >10M blocks, configure a deeper
  shard function (e.g. `next-to-last/3`).
- **HTTP/2 + connection reuse.** A tuned `http.Transport` (HTTP/2 attempt,
  `MaxIdleConnsPerHost: 256`, long keep-alive) amortizes connection setup across
  the many small requests; concurrency is bounded by an explicit semaphore.
- **One round-trip per block.** Blocks are written with a direct, idempotent
  `PUT` (no temp+rename), and `MKCOL` for each shard directory is issued at most
  once per process.
- **Local existence/size cache** to avoid `PROPFIND` round-trips for repeated
  `Has`/`GetSize`. Enable kubo's blockstore bloom filter for the negative path
  (see below).
- **Bounded retry with jittered backoff** on transient 5xx / network blips.

## Multiple instances around one WebDAV

Each kubo instance keeps its **own local repo** (peer identity, `config`,
`keystore`, `repo.lock`) and points at the **same** WebDAV `url` + `rootDirectory`.
Only the datastore contents are shared.

- **Blocks are safe to share with no coordination** — same CID ⇒ identical
  bytes ⇒ concurrent writes are harmless.
- **Mutable state is the one caveat.** Different pins are different datastore
  keys, so concurrent pinning of *different* CIDs is safe; but the single MFS
  root key (`ipfs files`) is last-write-wins across instances.

**Recommended topology:** one **writer / GC coordinator** node, and N
**read-mostly followers** configured with `"noDelete": true`. Run garbage
collection only on the coordinator (deletion is the only operation that can
remove a block another node still references).

## Install

The plugin ships three ways. Prefer the **bundled** binary/image — Go's runtime
plugin (`.so`) must be built with the exact kubo version *and* Go toolchain of
the host `ipfs`, so it is fragile.

### Bundled Docker image (recommended)

```bash
docker pull ghcr.io/ulbwa/kubo-ds-webdav:v0.37.0   # kubo + webdavds compiled in
```

### Bundled binary

Download `kubo-ds-webdav_kubo-<version>_linux-<arch>` from the
[Releases](https://github.com/ulbwa/kubo-ds-webdav/releases) — a drop-in `ipfs`
with `webdavds` compiled in (linux amd64 + arm64).

### `.so` plugin (linux/amd64 only)

Download `kubo-ds-webdav_<tag>_linux-amd64.so`, drop it into `~/.ipfs/plugins/`,
and run the **matching official kubo release**. The `.so` is published for
linux/amd64 only (Go plugins are restricted, and the ABI must match exactly).

## Configure the datastore

After `ipfs init`, point the datastore at WebDAV. Set `Datastore.Spec` to a
single `webdavds` mount and write the matching `datastore_spec` fingerprint:

```bash
ipfs config --json Datastore.Spec '{
  "type": "webdavds",
  "url": "https://dav.example.com/remote.php/dav/files/ipfs",
  "rootDirectory": "kubo",
  "username": "${WEBDAV_USER}",
  "password": "${WEBDAV_PASS}"
}'
# The on-disk fingerprint = the DiskSpec (sorted keys; no credentials):
printf '%s' '{"rootDirectory":"kubo","type":"webdavds","url":"https://dav.example.com/remote.php/dav/files/ipfs"}' \
  > "$IPFS_PATH/datastore_spec"

WEBDAV_USER=ipfs WEBDAV_PASS=secret ipfs daemon
```

> Set `Datastore.Spec` on a **fresh** repo. Migrating an existing repo to a
> different datastore requires a datastore migration, not just a config edit.

### Config fields

| Field | Required | Default | Notes |
|---|---|---|---|
| `url` | yes | — | WebDAV base URL |
| `rootDirectory` | no | "" | sub-collection this datastore owns |
| `username` / `password` | no | — | HTTP Basic auth; `${ENV}` expanded |
| `headers` | no | — | extra request headers; values `${ENV}` expanded |
| `shardFunc` | no | `…/next-to-last/2` | flatfs shard spec for block keys; immutable once written |
| `concurrency` | no | 32 | max in-flight WebDAV requests |
| `connTimeout` | no | 10s | dial + TLS handshake |
| `requestTimeout` | no | 60s | per-request timeout |
| `useMove` | no | auto | force temp-PUT+MOVE atomic writes for mutable keys |
| `noDelete` | no | false | reject `Delete` (set on follower nodes) |

Credentials and tunables are deliberately **excluded** from the `datastore_spec`
fingerprint, so rotating a password never triggers a spurious spec mismatch.

### Recommended kubo settings for large repos

- `Datastore.BloomFilterSize` (off by default) — sizes the blockstore bloom
  filter so "this block is absent" is answered from RAM instead of a WebDAV
  round-trip. ≈ 1.2 MB for 1M blocks, ≈ 16 MB for 10M (1% false-positive rate).
- `Datastore.BlockKeyCacheSize` — bump for large working sets.

## Develop / test

```bash
make test          # fast hermetic unit tests (in-process WebDAV, no Docker)
make integration   # go-datastore conformance suite vs a dockerized Apache WebDAV
make e2e           # two kubo instances sharing one WebDAV (acceptance test)
make plugin        # build the .so locally
```

The plugin/engine is split into a kubo-agnostic engine package (`webdavds`) and
a thin `plugin/` package that registers it with kubo. Tests run against a
dockerized Apache `mod_dav` server (`docker/Dockerfile.webdav`); the plugin works
with any RFC-4918 WebDAV server (Nextcloud, Apache, rclone, …).

## Releases / CI

- **`release.yml`** runs on a `v*` tag: tests, then publishes the linux/amd64
  `.so`, bundled images (amd64+arm64) to GHCR, and bundled binaries.
- **`watch-kubo.yml`** runs weekly: when a new stable kubo release appears it
  triggers a build for it. Tags are `v<kubo-version>+build.<N>`.

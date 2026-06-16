#!/bin/sh
# e2e entrypoint: configure a kubo repo to use the webdavds datastore (single
# mount at /), then exec the daemon. Each node keeps its own LOCAL repo
# (identity, config, repo.lock) but points at the SAME shared WebDAV datastore.
set -e

: "${IPFS_PATH:=/data/ipfs}"
export IPFS_PATH
: "${WEBDAV_URL:?WEBDAV_URL required}"
: "${WEBDAV_ROOT:?WEBDAV_ROOT required}"
mkdir -p "$IPFS_PATH"

if [ ! -f "$IPFS_PATH/config" ]; then
  ipfs init >/dev/null 2>&1
fi

# Point the whole datastore at WebDAV. noDelete marks read-mostly followers.
if [ -n "${WEBDAV_NODELETE:-}" ]; then
  ipfs config --json Datastore.Spec \
    "{\"type\":\"webdavds\",\"url\":\"${WEBDAV_URL}\",\"rootDirectory\":\"${WEBDAV_ROOT}\",\"noDelete\":true}"
else
  ipfs config --json Datastore.Spec \
    "{\"type\":\"webdavds\",\"url\":\"${WEBDAV_URL}\",\"rootDirectory\":\"${WEBDAV_ROOT}\"}"
fi

# The datastore_spec fingerprint = DiskSpec() with keys sorted (json.Marshal of
# a map), and excludes credentials / tunables / noDelete.
printf '%s' "{\"rootDirectory\":\"${WEBDAV_ROOT}\",\"type\":\"webdavds\",\"url\":\"${WEBDAV_URL}\"}" \
  > "$IPFS_PATH/datastore_spec"

exec ipfs "$@"

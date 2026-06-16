#!/bin/sh
# Entrypoint for the bundled kubo-ds-webdav image.
#
# It initializes a repo only when none exists yet (so the image runs on an empty
# volume), optionally applies a datastore profile, then execs `ipfs`. It does
# NOT configure the webdavds datastore for you — mount a pre-configured repo, or
# set Datastore.Spec yourself (see the README). The webdavds plugin is compiled
# in, so `ipfs` recognizes the "webdavds" datastore type out of the box.
set -e

: "${IPFS_PATH:=/data/ipfs}"
export IPFS_PATH
mkdir -p "$IPFS_PATH"

if [ ! -f "$IPFS_PATH/config" ]; then
  ipfs init ${IPFS_INIT_PROFILE:+--profile "$IPFS_INIT_PROFILE"}
fi

exec ipfs "$@"

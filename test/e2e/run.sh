#!/usr/bin/env bash
# End-to-end acceptance test:
#   1. boot a kubo daemon (node A) whose datastore is a shared WebDAV server
#   2. `ipfs add` a file on A
#   3. assert the blocks physically exist in WebDAV storage
#   4. boot a SECOND, offline kubo node (B) on the SAME WebDAV
#   5. assert B reads the content straight from shared storage (no bitswap)
set -euo pipefail
cd "$(dirname "$0")"

COMPOSE=(docker compose -f docker-compose.e2e.yml)
cleanup() { "${COMPOSE[@]}" down -v >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "==> Building bundled kubo image and starting WebDAV + node A"
"${COMPOSE[@]}" up -d --build webdav kubo-a

wait_daemon() { # $1 = service
  echo "    waiting for $1 daemon..."
  for _ in $(seq 1 60); do
    # Probe the daemon's HTTP API. Do NOT use `ipfs id`: with no daemon yet it
    # runs offline and grabs repo.lock, which then blocks the starting daemon.
    if "${COMPOSE[@]}" exec -T "$1" wget -qO- --post-data='' http://127.0.0.1:5001/api/v0/id >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "FAIL: $1 daemon did not become ready"; "${COMPOSE[@]}" logs "$1" | tail -30; exit 1
}
wait_daemon kubo-a

MSG="hello-from-kubo-ds-webdav-$(date +%s)"
echo "==> Adding a file on node A: \"$MSG\""
CID=$("${COMPOSE[@]}" exec -T kubo-a sh -c "printf '%s' '$MSG' | ipfs add -q --cid-version=1")
CID=$(echo "$CID" | tr -d '\r\n')
echo "    CID = $CID"
[ -n "$CID" ] || { echo "FAIL: empty CID"; exit 1; }

echo "==> Asserting blocks are physically stored in WebDAV"
DAV_ROOT=/usr/local/apache2/htdocs
N=$("${COMPOSE[@]}" exec -T webdav sh -c "find $DAV_ROOT/shared/blocks -type f 2>/dev/null | wc -l" | tr -d '[:space:]')
echo "    block files under WebDAV $DAV_ROOT/shared/blocks: $N"
[ "${N:-0}" -gt 0 ] || { echo "FAIL: no block files found in WebDAV"; exit 1; }

echo "==> Starting node B (offline) on the same WebDAV"
"${COMPOSE[@]}" up -d kubo-b
wait_daemon kubo-b

echo "==> Reading the content on node B (offline => only shared storage, no bitswap)"
OUT=$("${COMPOSE[@]}" exec -T kubo-b sh -c "ipfs cat $CID")
OUT=$(echo "$OUT" | tr -d '\r\n')
echo "    node B read: \"$OUT\""

if [ "$OUT" = "$MSG" ]; then
  echo
  echo "PASS ✅  Blocks are physically in WebDAV, and a second kubo instance"
  echo "        reads the content straight from the shared WebDAV datastore."
else
  echo "FAIL: node B content mismatch (got \"$OUT\", want \"$MSG\")"
  exit 1
fi

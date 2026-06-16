#!/usr/bin/env bash
# Re-pin this module's go.mod to a specific kubo release, aligning the go
# directive and the kubo dependency so the plugin .so is ABI-compatible with
# that kubo build.
#
#   ./set-target.sh v0.37.0
#
# The bundled image/binary builds (see Dockerfile) don't need this — they wire
# the plugin into kubo's own go.mod via `go mod edit -replace`. This script is
# for local development and for producing a matching standalone .so.
set -euo pipefail

KUBO_VERSION="${1:?usage: set-target.sh <kubo-version, e.g. v0.37.0>}"

# kubo's own go.mod tells us the Go toolchain it expects.
GO_DIRECTIVE=$(curl -fsSL "https://raw.githubusercontent.com/ipfs/kubo/${KUBO_VERSION}/go.mod" \
  | awk '/^go [0-9]/{print $2; exit}')
: "${GO_DIRECTIVE:?could not read go directive from kubo ${KUBO_VERSION}}"

echo "Targeting kubo ${KUBO_VERSION} (go ${GO_DIRECTIVE})"
go mod edit -go="${GO_DIRECTIVE}"
go get "github.com/ipfs/kubo@${KUBO_VERSION}"
go mod tidy

echo "Done. go.mod now targets kubo ${KUBO_VERSION}."

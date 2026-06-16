// Command plugin-main is the -buildmode=plugin entrypoint. It re-exports the
// Plugins symbol so `go build -buildmode=plugin` produces a kubo-loadable .so.
//
//	CGO_ENABLED=1 go build -buildmode=plugin -trimpath -o kubo-ds-webdav.so ./plugin-main
package main

import plugin "github.com/ulbwa/kubo-ds-webdav/plugin"

// Plugins is the symbol kubo's plugin loader resolves in the .so.
var Plugins = plugin.Plugins //nolint

// main is unused: this package is built with -buildmode=plugin, where main is
// never called. It exists only so a plain `go build ./...` succeeds.
func main() {}

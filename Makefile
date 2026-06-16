SHELL := /bin/bash

# The .so must be built with the same Go toolchain as the kubo binary it loads
# into. go.mod pins `toolchain go1.25.x`; override GO_VERSION to match a
# specific kubo release when producing a release artifact.
PLUGIN_OUT ?= kubo-ds-webdav.so
WEBDAV_TEST_URL ?= http://127.0.0.1:8091

.PHONY: build test integration e2e plugin clean webdav-up webdav-down

## build: compile all packages.
build:
	go build ./...

## test: run the fast, hermetic unit tests (no Docker).
test:
	go test ./...

## plugin: build the loadable .so (linux/amd64 in CI; native locally).
plugin:
	CGO_ENABLED=1 go build -buildmode=plugin -trimpath -o $(PLUGIN_OUT) ./plugin-main

## webdav-up: start the dockerized Apache WebDAV test server.
webdav-up:
	docker compose up -d --build --wait webdav

## webdav-down: stop and remove it.
webdav-down:
	docker compose down -v

## integration: run the go-datastore conformance suite against real WebDAV.
integration: webdav-up
	WEBDAV_TEST_URL=$(WEBDAV_TEST_URL) go test -tags integration -timeout 600s ./...

## e2e: two-instance end-to-end test (blocks physically in WebDAV, shared store).
e2e:
	bash test/e2e/run.sh

clean:
	rm -f $(PLUGIN_OUT)
	go clean

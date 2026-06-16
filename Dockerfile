# Builds kubo with the webdavds datastore compiled in (bundled via kubo's
# preload mechanism). No external .so, no CGO, no runtime plugin loading — a
# single self-contained `ipfs` binary that understands the "webdavds" datastore.
ARG GO_VERSION=1.25

# Build natively on the builder arch and cross-compile to the target arch.
# CGO is disabled, so cross-compiling is trivial.
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION} AS build
ARG KUBO_VERSION=v0.37.0
ARG TARGETARCH

# The plugin source (this repo) is copied in and wired into kubo via a replace
# directive, so the image always matches the committed code.
COPY . /src/kubo-ds-webdav

RUN git clone --depth 1 --branch "${KUBO_VERSION}" https://github.com/ipfs/kubo /kubo
WORKDIR /kubo
RUN go mod edit -replace github.com/ulbwa/kubo-ds-webdav=/src/kubo-ds-webdav \
 && printf '\nwebdavds github.com/ulbwa/kubo-ds-webdav/plugin 0\n' >> plugin/loader/preload_list \
 && make plugin/loader/preload.go \
 && go mod tidy

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w" -o /ipfs ./cmd/ipfs

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /ipfs /usr/local/bin/ipfs
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
ENV IPFS_PATH=/data/ipfs
VOLUME /data/ipfs
# 4001 swarm, 5001 RPC API, 8080 gateway
EXPOSE 4001 5001 8080
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["daemon", "--migrate=true"]

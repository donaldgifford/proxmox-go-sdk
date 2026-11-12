# syntax=docker/dockerfile:1.29@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183
# Builds the mockpve server image: an in-memory Proxmox VE responder for
# consumer integration tests (e.g. `docker run` a fake PVE in CI).
#
# NOTE: this is NOT a service image for the SDK. proxmox-go-sdk is a Go library
# consumed via its module path, not a running binary. The only runnable artifact
# this repo ships is the mockpve test helper (cmd/mockpve).
#
# Multi-stage build: small distroless image, cached module + build layers.

FROM golang:1.27.4@sha256:5e8afd493a7124a8586e134d9b532731161d4b9becf9dee5a5956b107426f93d AS build
WORKDIR /src
COPY go.* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/mockpve ./cmd/mockpve

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mockpve /usr/local/bin/mockpve
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/mockpve"]

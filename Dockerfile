# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
# Builds the mockpve server image: an in-memory Proxmox VE responder for
# consumer integration tests (e.g. `docker run` a fake PVE in CI).
#
# NOTE: this is NOT a service image for the SDK. proxmox-go-sdk is a Go library
# consumed via its module path, not a running binary. The only runnable artifact
# this repo ships is the mockpve test helper (cmd/mockpve).
#
# Multi-stage build: small distroless image, cached module + build layers.

FROM golang:1.26.10@sha256:672c944cfd07599b5f1bb179869270cdc94c6254e41db4d431259693cb286b82 AS build
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

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/mockpve /usr/local/bin/mockpve
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/mockpve"]

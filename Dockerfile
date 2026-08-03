# Base images are pinned by digest, not by tag. A moving tag means the image
# you tested is not necessarily the image you deployed.
FROM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build

ARG VERSION=dev
ARG COMMIT=unknown

WORKDIR /src

# Dependencies first, so a source-only change does not re-download the module
# graph.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO off gives a static binary, so the runtime image needs no libc.
ENV CGO_ENABLED=0
RUN go build -trimpath \
      -ldflags "-s -w \
        -X github.com/theorigamicorporation/toc-whmcs-mcp/internal/version.Version=${VERSION} \
        -X github.com/theorigamicorporation/toc-whmcs-mcp/internal/version.Commit=${COMMIT}" \
      -o /out/toc-whmcs-mcp ./cmd/toc-whmcs-mcp

# distroless static: no shell, no package manager, no interpreter. There is
# nothing in this image to run except the server, which matters for something
# holding a credential to a billing system.
FROM gcr.io/distroless/static-debian12@sha256:a9fcaedd4c9b59e12dd65d954f0b5044f19b0647a8a3712e77205df9e7b102cd

ARG VERSION=dev
ARG COMMIT=unknown

LABEL org.opencontainers.image.title="toc-whmcs-mcp" \
      org.opencontainers.image.description="MCP server for the WHMCS Admin API" \
      org.opencontainers.image.vendor="The Origami Corporation" \
      org.opencontainers.image.licenses="Proprietary" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/theorigamicorporation/toc-whmcs-mcp"

COPY --from=build /out/toc-whmcs-mcp /toc-whmcs-mcp

USER nonroot:nonroot

# A real check: it calls WHMCS with the configured credential and fails if the
# instance is unreachable or the credential is rejected. A check that only
# proves the runtime can execute a statement reports healthy while the server
# is unable to do anything at all.
HEALTHCHECK --interval=60s --timeout=15s --start-period=10s --retries=3 \
    CMD ["/toc-whmcs-mcp", "-healthcheck"]

ENTRYPOINT ["/toc-whmcs-mcp"]

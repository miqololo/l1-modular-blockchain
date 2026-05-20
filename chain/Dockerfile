# syntax=docker/dockerfile:1.6
ARG GO_VERSION=1.22.5

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates

COPY go.mod ./
COPY go.sum* ./
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/aid ./cmd/aid

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /out/aid /usr/local/bin/aid

# Phase 0.5 runs as root for volume-permission simplicity.
# Production deployments should use a dedicated non-root user with
# pre-chowned named volumes (handled by an init container).
ENV AID_HOME=/home/aios/.aid
RUN mkdir -p /home/aios
WORKDIR /home/aios

EXPOSE 26657

HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=5 \
  CMD wget -qO- http://localhost:26657/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/aid"]
CMD ["--home", "/home/aios/.aid", "--listen", ":26657"]

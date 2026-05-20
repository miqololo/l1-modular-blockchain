# syntax=docker/dockerfile:1.6
ARG GO_VERSION=1.22.5

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates

COPY go.mod ./
COPY go.sum* ./
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/inferenced ./cmd/inferenced

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/inferenced /usr/local/bin/inferenced

HEALTHCHECK --interval=15s --timeout=3s --start-period=30s --retries=3 \
  CMD pidof inferenced >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/usr/local/bin/inferenced"]

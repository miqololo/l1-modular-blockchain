# syntax=docker/dockerfile:1.6
ARG GO_VERSION=1.22.5

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git ca-certificates

COPY go.mod ./
COPY go.sum* ./
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/indexerd ./cmd/indexerd

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget
COPY --from=builder /out/indexerd /usr/local/bin/indexerd

RUN mkdir -p /data
WORKDIR /data

EXPOSE 8081

HEALTHCHECK --interval=5s --timeout=3s --start-period=10s --retries=5 \
  CMD wget -qO- http://localhost:8081/health >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/indexerd"]
CMD ["--db", "/data/indexer.db", "--listen", ":8081"]

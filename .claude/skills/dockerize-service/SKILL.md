---
name: dockerize-service
description: Wrap a service in a multi-stage Dockerfile and add it to the root docker-compose.yml with a healthcheck and proper depends_on wiring. Use when adding a new service to the demo stack or productionizing an existing one. Enforces image-size budgets and security defaults.
---

# Dockerize a Service

A disciplined recipe for taking a service (Go binary, Next.js app, etc.) and making it part of the `docker compose up` demo stack.

## Inputs needed

1. **Service name** (matches the package directory, e.g. `inference-node`)
2. **Language / runtime** (go / nextjs / postgres / external)
3. **Ports it exposes** (or none)
4. **Health endpoint** (HTTP path / TCP port / CLI command)
5. **Dependencies** (other services it `depends_on`)
6. **Environment variables** it needs

## Steps

### Step 1 — Multi-stage Dockerfile

#### For Go services

```dockerfile
# <service>/Dockerfile
# syntax=docker/dockerfile:1.6
ARG GO_VERSION=1.22.5
FROM golang:${GO_VERSION}-alpine@sha256:<digest> AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /out/<binary> ./cmd/<binary>

FROM gcr.io/distroless/static-debian12:nonroot@sha256:<digest>
COPY --from=builder /out/<binary> /<binary>
USER nonroot:nonroot
EXPOSE <port>
HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 \
  CMD ["/<binary>", "health"] || exit 1
ENTRYPOINT ["/<binary>"]
```

Pin digests with `docker buildx imagetools inspect golang:1.22.5-alpine` and copy the SHA.

#### For Next.js

Requires `next.config.js` with `output: 'standalone'`.

```dockerfile
# frontend/Dockerfile
# syntax=docker/dockerfile:1.6
FROM node:20-alpine@sha256:<digest> AS deps
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile

FROM node:20-alpine@sha256:<digest> AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN corepack enable && pnpm build

FROM node:20-alpine@sha256:<digest> AS runner
WORKDIR /app
ENV NODE_ENV=production
RUN addgroup -g 1001 nodejs && adduser -u 1001 -G nodejs -S nextjs
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public
USER nextjs
EXPOSE 3000
ENV PORT=3000 HOSTNAME=0.0.0.0
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s \
  CMD wget -qO- http://localhost:3000/api/health || exit 1
CMD ["node", "server.js"]
```

### Step 2 — Add to docker-compose.yml

```yaml
services:
  <service-name>:
    build:
      context: ./<service-name>
      dockerfile: Dockerfile
    image: aios/<service-name>:dev
    container_name: aios-<service-name>
    restart: unless-stopped
    ports:
      - "${<SERVICE>_PORT:-<port>}:<port>"
    environment:
      KEY: value
    depends_on:
      <upstream>:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "/<binary>", "health"]
      interval: 10s
      timeout: 3s
      start_period: 15s
      retries: 3
    networks:
      - aios
```

### Step 3 — Add health endpoint (if HTTP service)

Go services: add `/health` returning `{"status":"ok"}` with 200 if all dependencies (DB, upstream) are reachable, 503 otherwise.

Next.js: `src/app/api/health/route.ts`:

```ts
// frontend/src/app/api/health/route.ts
export async function GET() {
  return Response.json({ status: 'ok' }, { status: 200 });
}
```

### Step 4 — Smoke test

Add to root `Makefile`:

```makefile
.PHONY: compose-smoke
compose-smoke: ## Bring up the stack, hit health endpoints, tear down
	docker compose up -d
	./scripts/wait-healthy.sh
	curl -fsS http://localhost:3000/api/health
	curl -fsS http://localhost:8081/health
	docker compose down -v
```

Run it locally. If any healthcheck fails or any endpoint 5xxs, the dockerization is incomplete.

### Step 5 — Image size budget check

```bash
docker images aios/<service-name>:dev --format "{{.Size}}"
```

Budgets (.claude/CLAUDE.md mandate):
- Go services: ≤ 50 MB
- Next.js: ≤ 200 MB
- llama-server: passthrough (no budget)

If exceeded, investigate: are you copying source code into the runtime image? Are you using a fat base image?

### Step 6 — Lint

```bash
hadolint <service>/Dockerfile
```

Fix every warning. `DL3018` (apk-add without version) — pin versions. `DL3008` (apt without version) — same.

### Step 7 — Update `.env.example`

Add any new env vars your service needs:

```
# inference-node
INFERENCE_NODE_LOG_LEVEL=info
LLAMA_SERVER_URL=http://llama-server:8080
CHAIN_RPC=http://chain:26657
```

## Forbidden

- `latest` tags or unpinned digests in committed Dockerfiles
- `apt-get install` without version pins in the final image
- Running as root in the runtime image
- Hardcoded secrets in `.env.example` (use placeholders)
- `COPY . .` without a tight `.dockerignore` — leaks `.git`, `node_modules`, test data
- `RUN curl ... | sh` patterns
- Long-running entrypoints without a healthcheck
- `depends_on:` without `condition: service_healthy` for services that need an upstream to be ready

## Output format

```
## What changed
- <service>/Dockerfile — new
- docker-compose.yml — added <service> service block
- Makefile — extended compose-smoke
- .env.example — added <vars>

## Image size
- aios/<service>:dev — XX MB (budget: YY)

## Smoke test
- make compose-smoke: PASS / FAIL
```

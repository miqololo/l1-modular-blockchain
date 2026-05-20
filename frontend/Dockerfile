# syntax=docker/dockerfile:1.6
# Build context is the project root. Paths are relative to the project root.

FROM node:20-alpine AS deps
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml* ./
RUN pnpm install --no-frozen-lockfile

FROM node:20-alpine AS builder
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate

# NEXT_PUBLIC_* variables are inlined into the client bundle at build time —
# they CANNOT be overridden at runtime via docker env. Pass them as build args
# from docker-compose so the production browser bundle calls the right
# externally-reachable URLs (e.g. http://178.105.144.11:8081), not
# http://localhost:8081 which would only work for someone browsing on the
# same host. Defaults preserve the make-demo flow.
ARG NEXT_PUBLIC_INDEXER_URL=http://localhost:8081
ARG NEXT_PUBLIC_CHAIN_URL=http://localhost:26657
ENV NEXT_PUBLIC_INDEXER_URL=${NEXT_PUBLIC_INDEXER_URL} \
    NEXT_PUBLIC_CHAIN_URL=${NEXT_PUBLIC_CHAIN_URL}

COPY --from=deps /app/node_modules ./node_modules
COPY frontend/ ./
# Ship the project documentation alongside the app so the in-app /docs route
# can serve the markdown straight off disk at request time.
COPY docs/ /app/docs-source/
RUN pnpm build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
RUN addgroup -g 1001 -S nodejs && adduser -S nextjs -u 1001 -G nodejs && \
    apk add --no-cache wget

COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public
# Docs read at request time by app/docs/[slug]/page.tsx via fs.readFile.
COPY --from=builder --chown=nextjs:nodejs /app/docs-source /app/docs-source

USER nextjs
EXPOSE 3000
ENV PORT=3000 HOSTNAME=0.0.0.0

HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 \
  CMD wget -qO- http://localhost:3000/api/health >/dev/null || exit 1

CMD ["node", "server.js"]

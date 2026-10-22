# ---- Stage 1: Build frontend ----
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder
ARG SOURCE_REVISION=master
ARG SOURCE_URL

WORKDIR /app/frontend

# corepack reads the pinned pnpm version from frontend/package.json
# (packageManager). pnpm resolves the correct per-platform optional native
# bindings (esbuild / rollup / tailwind-oxide, incl. linux-arm64-musl) from the
# lockfile, so --frozen-lockfile is safe for the native amd64 + arm64 CD matrix.
RUN corepack enable
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY frontend/ ./
RUN test -n "$SOURCE_URL" \
      && case "$SOURCE_URL" in https://*) ;; *) echo "SOURCE_URL must be HTTPS" >&2; exit 1 ;; esac \
      && case "$SOURCE_URL" in https://github.com/TokenDanceLab/tokendance-chat|https://github.com/TokenDanceLab/tokendance-chat/*) echo "SOURCE_URL must be anonymously accessible; the private development repository is not a valid release source" >&2; exit 1 ;; esac \
      && VITE_SOURCE_URL="$SOURCE_URL" pnpm run build

# ---- Stage 2: Build backend ----
FROM golang:1.26.10-alpine AS backend-builder

WORKDIR /app/backend

COPY backend/go.mod backend/go.sum* ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server .

# ---- Stage 3: Runtime ----
FROM alpine:3.23
ARG SOURCE_REVISION=master
ARG SOURCE_URL
RUN test -n "$SOURCE_URL"
LABEL org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.revision="${SOURCE_REVISION}"

RUN apk add --no-cache ca-certificates tzdata

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /app

COPY --from=frontend-builder /app/frontend/dist /app/frontend/dist
COPY --from=backend-builder /app/server /app/server
COPY LICENSE NOTICE /app/
COPY LICENSES/ /app/LICENSES/

# Create data directory with correct ownership.
RUN mkdir -p /app/data && chown -R appuser:appgroup /app/data /app/frontend

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD sh -ec 'port="${CHAT_ADDR:-:8080}"; port="${port##*:}"; wget -qO- "http://127.0.0.1:${port}/api/health" >/dev/null'

CMD ["/app/server"]

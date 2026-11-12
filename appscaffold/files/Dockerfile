# syntax=docker/dockerfile:1.28

FROM node:26-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
# pnpm comes from npm at the version package.json names: Node 25 and later ship no corepack.
RUN npm install -g "$(node -p 'require("./package.json").packageManager')"
# CI passes CACHE_SCOPE (deliver or verify): pull-request builds never write the deliveries' caches.
ARG CACHE_SCOPE=local
RUN --mount=type=cache,id=pnpm-${CACHE_SCOPE},target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run typecheck && pnpm run build

FROM golang:1.27.3-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
COPY web/ ./web/
COPY --from=frontend /src/web/dist ./web/dist
# Registry-owned frontend files must match the pinned app-foundation (sync after a bump).
RUN go tool frontend-registry check -target web -modules __CHECKED_MODULES__
ARG CACHE_SCOPE=local
RUN --mount=type=cache,id=gobuild-${CACHE_SCOPE},target=/root/.cache/go-build \
    test -z "$(gofmt -l cmd internal migrations web)" && \
    go vet -mod=vendor ./... && \
    CGO_ENABLED=0 go test -mod=vendor ./...
RUN --mount=type=cache,id=gobuild-${CACHE_SCOPE},target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -mod=vendor -p 2 -trimpath \
    -ldflags='-s -w' -o /out/app ./cmd/__APP_NAME__

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=builder /out/app /app
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app"]

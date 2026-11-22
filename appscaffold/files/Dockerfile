# syntax=docker/dockerfile:1.29@sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183

FROM node:26-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS frontend
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

FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder
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

# Phone audit (e2e/phone.spec.ts, registry module phone-audit): the built UI in Chromium at phone
# sizes; a finding fails the build. The image's version is @playwright/test's in web/package.json
# (its browser is in the image); the runtime stage copies the marker, so the stage always runs.
FROM mcr.microsoft.com/playwright:v1.65.0-noble@sha256:9beb546099e11f52d62e505537914870e01a67e0e6b00ae41da4775324e73dbe AS phone
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN npm install -g "$(node -p 'require("./package.json").packageManager')"
ARG CACHE_SCOPE=local
RUN --mount=type=cache,id=pnpm-${CACHE_SCOPE},target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
COPY --from=frontend /src/web/dist ./dist
RUN CI=1 pnpm run test:phone && touch /phone-audit.ok

FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
COPY --from=phone /phone-audit.ok /phone-audit.ok
COPY --from=builder /out/app /app
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app"]

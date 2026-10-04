# syntax=docker/dockerfile:1.7

FROM node:24-alpine AS frontend
RUN corepack enable && corepack prepare pnpm@10.0.0 --activate
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,id=pnpm,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run typecheck && pnpm run build

FROM golang:1.26.7-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
COPY web/ ./web/
COPY --from=frontend /src/web/dist ./web/dist
RUN test -z "$(gofmt -l cmd internal migrations web)" && \
    go vet -mod=vendor ./... && \
    CGO_ENABLED=0 go test -mod=vendor ./...
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -p 2 -trimpath \
    -ldflags='-s -w' -o /out/app ./cmd/...

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/app /app
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app"]

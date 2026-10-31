# Migrating to v1.0.0 (platform-upstream)

v1.0.0 is the first release under the neutral module path. It contains everything of
`castillo-platform/app-foundation` v0.21.0 and `niiku-platform/app-foundation` up to 8eb1954
(`agentkit.GuardRemote`, `access.Remote`). The API is unchanged apart from `appscaffold`.

In an app:

```bash
go env -w GOPRIVATE=github.com/platform-upstream/*
grep -rl 'github.com/\(castillo\|niiku\)-platform/app-foundation' --include='*.go' --include=go.mod . \
  | xargs sed -i 's#github.com/\(castillo\|niiku\)-platform/app-foundation#github.com/platform-upstream/app-foundation#g'
go get github.com/platform-upstream/app-foundation@v1.0.0
go mod tidy && go mod vendor && gofmt -w cmd internal
go tool frontend-registry check -target web -modules api,pwa,pwa-shell   # sync + review if it differs
```

`appscaffold.Options` needs a `Platform` (org, domain, registry, Access team); the CLI reads it
from `-platform platform.json`. `reference-app` is gone; the scaffold test covers its role.

# Holiaokho (hó-liāu-khòo, "the good-stuff store")

Pronounced: ho-LIAO-kho · CLI: `holiao`

好料庫 — a self-hosted artifact repository manager built as a drop-in
replacement for Sonatype Nexus Repository OSS/CE: same repository URLs, same
Docker port connectors, same accounts, so existing CI/CD keeps working after
the swap. Single Go binary, PostgreSQL, local or S3-compatible blob storage.

## Status

Early backend (P0–P3 of `docs/holiaokho-architecture-draft.md` §14):

| Area | State |
|---|---|
| Maven 2 hosted / proxy / group (metadata merge, snapshots, write policies) | ✅ tested with `mvn` |
| npm hosted / proxy / group (publish, login, dist-tags, audit pass-through) | ✅ tested with `npm` |
| Docker / OCI hosted / proxy / group, port + path connectors, daemon `registry-mirrors` mode | ✅ tested with `dockerd` |
| Users, roles (RBAC), user tokens, sessions, login rate limiting, Nexus (Shiro) password hashes | ✅ |
| Management REST API `/api/v1`, `holiao` CLI (en / zh-TW; zh-CN, ja, ko pending) | ✅ |
| Cleanup policies, blob GC, task scheduler, audit log, Prometheus `/metrics` | ✅ basic |
| `holiaokho import-nexus` (PostgreSQL + blob store, hardlink, idempotent) | ✅ tested against Nexus 3.95.3 |
| Web UI | ⏳ placeholder page; designed separately |
| S3 storage, NuGet/PyPI/other formats, LDAP/OIDC, webhooks | ⏳ see `docs/nexus-feature-parity.md` |

## Run

```sh
# PostgreSQL
docker run -d --name holiaokho-pg -e POSTGRES_USER=holiaokho -e POSTGRES_PASSWORD=holiaokho \
  -e POSTGRES_DB=holiaokho -p 5432:5432 postgres:16-alpine

go build -o bin/holiaokho ./cmd/holiaokho && go build -o bin/holiao ./cmd/holiao
./bin/holiaokho                      # http://localhost:8081, admin / admin123 (change it)
```

Or `docker compose -f deploy/docker-compose.yml up -d`, or `kubectl apply -f deploy/k8s/holiaokho.yaml`.
Configuration: `config.example.yaml` or `HOLIAOKHO_*` environment variables.

## Use

```sh
holiao login http://localhost:8081
holiao repo create maven-central --format maven --type proxy --remote https://repo1.maven.org/maven2/
holiao repo create maven-releases --format maven --type hosted --write-policy allow_once
holiao repo create maven-public --format maven --type group --members maven-releases,maven-central
holiao repo ls
```

Clients point at `http://host:8081/repository/<name>/` exactly as with Nexus.
Docker repositories get a dedicated port via `docker.httpPort` (Nexus style) and
are also reachable as `host:8081/<repo>/<image>` (path mode).

## Migrate from Nexus

```sh
holiaokho import-nexus \
  --nexus-db postgres://nexus:***@nexus-postgres:5432/nexus \
  --nexus-blobs /nexus-data/blobs --link
```

Reads Nexus read-only; can be re-run until cut-over. See architecture doc §16.

## Develop

```sh
scripts/dev-restart.sh          # build + restart local server on :18081
go test ./...
```

Docs: `docs/holiaokho-naming-handover.md`, `docs/holiaokho-architecture-draft.md`,
`docs/nexus-feature-parity.md`.

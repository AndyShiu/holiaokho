# Holiaokho (hó-liāu-khòo, "the good-stuff store")

Pronounced: ho-LIAO-kho · CLI: `holiao`

好料庫 — a self-hosted artifact repository manager built as a drop-in
replacement for Sonatype Nexus Repository OSS/CE: same repository URLs, same
Docker port connectors, same accounts, so existing CI/CD keeps working after
the swap. Single Go binary, PostgreSQL, local or S3-compatible blob storage.

## Status

Backend feature-complete against Nexus Repository 3 CE (see `docs/nexus-feature-parity.md`):

- **26 formats**, each verified with its official client in Docker (`scripts/e2e-formats.sh`): Maven, npm, Docker/OCI, PyPI, raw, NuGet, Helm, Go, APT, YUM, Alpine, RubyGems, Cargo, Composer, Conda, R/CRAN, p2, CocoaPods, Terraform, pub, Git LFS, Hugging Face, Ansible Galaxy, Conan, Swift.
- hosted / proxy / group for every format (groups forward publishes to their first hosted member where the client can only target one URL).
- Auth: local users (argon2; Nexus Shiro hashes accepted on import), LDAP, OIDC, reverse-proxy header (Rut), RBAC with content selectors, user tokens, login rate limiting.
- Ops: routing rules, cleanup policies (+ preview), soft-delete + compact GC, storage quotas, cron-scheduled tasks, webhooks (HMAC), e-mail, audit log, backup/restore (DB + blobs), rebuild-indexes, system info / logs / support zip, Prometheus metrics.
- Storage: local filesystem or any S3-compatible service. TLS on the main listener; Docker port / TLS / subdomain connectors.
- Migration: `holiaokho import-nexus` (PostgreSQL-backed Nexus 3.7x+, hardlinks blobs). `/service/rest/v1` compatibility subset for CI scripts.
- Web UI: not started (placeholder page); the management API and CLI cover everything.

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

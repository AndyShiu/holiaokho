# Contributing to Holiaokho

Thanks for looking. Bug reports, questions and patches are all welcome.

## Before a pull request: the CLA

Pull requests need a signed Contributor License Agreement. A bot will ask on
your first PR; signing is a one-off, takes a minute, and covers everything you
send afterwards.

The short version of what it says: you keep the copyright to what you write,
and you grant Pei-En Hsu the right to distribute it — including
under licences other than Apache 2.0. That last part is what lets the project
change direction later (dual licensing, a different licence for a future
version) without having to track down every contributor.

If that is not something you want to grant, please still open an issue. A good
bug report with a reproduction is worth more than most patches, and needs no
paperwork.

## Reporting a security problem

Please do **not** open a public issue for a security vulnerability. Use
GitHub's private reporting instead — the **Security** tab, then *Report a
vulnerability* — which reaches the maintainers without the details becoming
public first. See [SECURITY.md](SECURITY.md).

Holiaokho sits in the middle of a software supply chain: it serves the
dependencies other people build on. Vulnerabilities here matter more than the
severity score suggests, and they are taken seriously.

## Getting it running

Requires Go 1.27+, Node 22+, Docker, and a PostgreSQL to point at.

```sh
# Everything at once: postgres in docker, backend, frontend dev server
scripts/dev-env.sh

# Or by hand
docker run -d --name holiaokho-pg -p 55432:5432 \
  -e POSTGRES_USER=holiaokho -e POSTGRES_PASSWORD=holiaokho -e POSTGRES_DB=holiaokho \
  postgres:16-alpine

HOLIAOKHO_DATABASE_URL="postgres://holiaokho:holiaokho@localhost:55432/holiaokho?sslmode=disable" \
  go run ./cmd/holiaokho

cd web && npm install && npm run dev   # http://localhost:5173/ui/
```

## Tests

```sh
go test ./...            # unit tests, no external services needed
cd web && npm run build  # also runs the locale checks
scripts/e2e.sh           # real mvn / npm / docker clients; needs docker
```

`scripts/e2e.sh` is the one that matters for anything touching a package
format. It drives actual Maven, npm, pip and Docker clients against a running
server, because the formats are defined by what those clients do, not by what
their specifications say.

## What makes a change easy to accept

**Explain the failure, not the diff.** The commit message should say what went
wrong and under what conditions. We can read the code; what we cannot recover
later is why it needed to change.

**Match the surrounding code.** Comment density, naming, error handling — the
existing style is deliberate. Comments explain why something is the way it is,
not what the line does.

**Cover a new format with e2e.** A format plugin that passes unit tests but has
never been driven by its real client is not finished.

**Keep translations honest.** The UI ships five locales. If you add a string,
add it to `web/src/i18n/locales/en.json`; the others may fall back to English
rather than be machine-translated badly. `npm run build` fails on missing keys,
and on scripts that do not belong in a locale (this has caught a Russian word
inside a Japanese sentence).

## Things worth knowing

- The API returns a stable error `code` plus parameters; the UI does the
  localising. Never return a translated string from the backend.
- Storage is content-addressed and reference-counted. Anything that deletes
  blobs must go through the reference count, never the filesystem.
- Proxy repositories must keep serving cached content when the upstream is
  down. That behaviour is load-bearing, not a nicety.

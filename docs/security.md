English | [繁體中文](security.zh-TW.md)

# Holiaokho Security Model and Review Log

> Full backend review on 2026-09-13. This document describes the trust boundaries, the protections already implemented, the issues found and fixed during the review, and what to watch for at deployment time.

## Trust Boundaries

| Source | Trust level |
|---|---|
| Config files / environment variables / K8s Secret | Fully trusted (administrator) |
| `admin` and users with `app:*` permissions | Their administrative actions are trusted (setting proxy upstreams, webhook URLs, cleanup regexes, etc. can all affect server behavior — this is by design) |
| Users with repo `write` permission | Can upload content; **content is treated as untrusted** (see XSS protection) |
| Anonymous / `read` users | Can only read authorized repos; **all input is treated as untrusted** |
| Upstream registries (Maven Central, Docker Hub, …) | Semi-trusted: response content is cached and forwarded, but never executed; URLs in upstream metadata are only used for re-fetching (same as Nexus) |
| Reverse proxy | `X-Forwarded-For` is only trusted from peers within `server.trusted_proxies` |

## Protections Already Implemented

**Authentication and authorization**
- Passwords use argon2id; imported Nexus Shiro hashes are auto-upgraded after verification; comparisons use constant time
- Password complexity policy (`auth.settings.password`, tunable via API): defaults to a minimum of 12 characters, requiring uppercase, lowercase, and digits, disallowing the username, and rejecting common weak passwords; applied uniformly to user creation, admin resets, and self-service password changes, returning a `password.*` code and params on violation. The bootstrap admin password is not blocked but logs a warning
- User tokens are stored only as SHA-256; tokens in Docker/npm/NuGet/cargo/gem/conan and other formats all map to the same underlying token
- Login failures are rate-limited (per client IP, 10/minute by default, returns 429)
- RBAC: `target` × `actions`, with content selectors able to restrict by path; anonymous is an explicit role that can be disabled entirely
- Session cookies: HttpOnly, SameSite=Lax, Secure over HTTPS; a new session id is issued on login
- OIDC: state cookie prevents CSRF, `next` only allows in-site paths; LDAP filters are escaped; Rut Auth requires `trustedProxies` to be configured, otherwise the header is ignored
- All API calls go through permission checks; repos without access are filtered out of search results
- Deploying to a group (1.4.0): the upload is stored in the group's first hosted member, and the caller needs write permission on **both** the group and that member, so a group is never a way round a hosted repository's permissions
- Vulnerability data always requires signing in, even when anonymous access is on (1.3.3)

**Input handling**
- All SQL is parameterized; LIKE patterns are escaped
- Storage paths go through `CleanPath` (rejects `..`, empty segments, control characters); blobs are keyed by sha256, so users never touch physical paths
- Docker upload ids, npm package names, Docker image names, and LFS oids are all format-validated
- Uploads and upstream reads both have size limits; zip/tar parsing only reads the files it needs
- Upstream addresses come only from administrator configuration or metadata resolved by the server itself — **client-supplied URLs are never accepted** (fixed for the Terraform module and Go sumdb proxies)

**Browser protections**
- Site-wide `X-Content-Type-Options: nosniff`
- `/repository/*` and `/v2/*` responses add `Content-Security-Policy: sandbox; default-src 'none'`: uploaded HTML will not execute scripts under Holiaokho's origin in the browser (stored XSS protection)
- API/UI: `X-Frame-Options: DENY`, `Referrer-Policy`, and `default-src 'none'` CSP on the API
- Directory listing and simple index output are both HTML-escaped

**Secrets**
- Upstream passwords, signing private keys, LDAP/OIDC/SMTP passwords, and the S3 secretKey are encrypted with AES-256-GCM before being stored (`secrets.key`), with rotation support
- API responses always mask these as `***`; `/system/config` masks the DB URL, S3, admin password, and credentials embedded in `http_proxy`
- Backup files contain ciphertext and require the same key to be usable
- TLS minimum version is 1.2

**Supply chain: malicious packages (1.4.0)**
- Packages OSV lists as malicious (`MAL-` records from the OpenSSF malicious-packages database, and advisories classed CWE-506) are refused at download (403 `package.malicious`), from the cache and from proxy upstreams alike; other members of a group do not serve them instead
- A package the scan has checked is judged from that result; a new one is looked up in OSV on its first request (5-second timeout, one lookup however many requests arrive together, verdict cached for an hour)
- **When OSV cannot be reached, downloads go ahead** (fail open) and a warning is logged: an internal network with no route out is a normal deployment, and failing closed would stop every build. Where failing closed matters, point `vulnerabilities.osv_url` at an internal OSV mirror
- Every block is recorded (package, repository, user, count); the first is sent by email and as a `package.blocked` webhook
- Only `app:system write` can allow a listed version, with a required reason; allowing and withdrawing are both in the audit log
- This matches a list of known malicious packages; it is not behavioural analysis. A malicious package not yet listed is not stopped

## 2026-09-13 Review Findings and Fixes

| Severity | Issue | Fix |
|---|---|---|
| High | Docker chunked upload's `uploads/<id>` was not validated → `../` could be used to read/write files outside the storage directory | id must now be a UUID; the storage layer adds a second `ValidUploadID` check |
| High | The Terraform module proxy accepted a client-supplied `?upstream=` → SSRF and cache poisoning | The download address is now re-resolved by the server against the upstream registry |
| Medium | The Go proxy's `/sumdb/<host>/…` forwarded to an arbitrary host | Only `sum.golang.org` / `sum.golang.google.cn` are now allowed |
| Medium | Uploading HTML to a hosted repo → stored XSS | Content responses now add CSP sandbox + nosniff |
| Medium | OIDC `next=//evil.com` open redirect | Values starting with `//` or `/\` are now rejected |
| Medium | `X-Forwarded-For` was trusted unconditionally → could be spoofed to bypass login rate limiting | Only peers within `trusted_proxies` are trusted, using the rightmost untrusted hop |
| Medium | Rut Auth allowed an empty `trustedProxies` (= trusting everyone) | With an empty list, the header is now ignored and a warning is logged |
| Low | Missing security headers, cookies without Secure, no TLS minimum version, `http_proxy` credentials not masked, npm names not validated, restore relying on superuser | All addressed |

## Deployment Notes

1. **`HOLIAOKHO_SECRET_KEY`** must be managed via K8s Secret and backed up; losing it means all upstream passwords and signing keys become undecryptable.
2. **`server.trusted_proxies`** should be narrowed to the actual ingress/proxy addresses; the default is a private network range (for easy in-cluster deployment), which means nodes on the same subnet can affect the IP used for rate-limit decisions.
3. Services exposed externally **must always sit behind TLS** (a reverse proxy or `server.tls_*`); Basic auth and tokens sent over plain HTTP can be intercepted.
4. **Change the admin password immediately** after first startup (the health check will keep warning otherwise).
5. Anonymous read access is enabled by default (to allow a frictionless replacement for Nexus); set `auth.anonymous_enabled: false` if it isn't needed.
6. `/metrics` and `/service/metrics/prometheus` do not require authentication (they only expose counters); block them at the ingress if you don't want them exposed.
7. Proxy upstreams and webhook URLs configured by administrators cause the server to actively connect to them — this is by design, but it means an admin account effectively lets the server make requests into the internal network; admin accounts must be tightly controlled.

## Not Yet Done (Known)

- No site-wide API rate limiting (only login is rate-limited)
- Docker foreign layers are fetched directly from upstream by the client (same as Nexus's default)
- URLs embedded in upstream metadata (Helm index, Composer dist, Ansible download_url, …) are fetched by the server: a malicious upstream could direct the server to a location of its choosing; proxy upstreams can only be configured by an admin, so the risk is equivalent to Nexus

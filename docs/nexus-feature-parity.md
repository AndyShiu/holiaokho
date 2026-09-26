English | [繁體中文](nexus-feature-parity.zh-TW.md)

# Nexus Repository Feature Parity

> Purpose: make "we have everything Nexus has" a trackable list, not just a slogan.
> Baseline: Nexus Repository 3.95 Community Edition. Features available only in the paid editions are listed separately in §7.
> Status column: `P0`–`P7` are implementation stage numbers; `Later` = scheduled but not yet staged; `Won't do` = explicitly not doing, with a reason given; ✅ = implemented and tested with the official client.
> First version 2026-09-13. Updated 2026-09-24 after verifying each item against the code (old `P`-stage markers replaced with actual status; §8 added).

---

## 1. Repository Formats

Formats supported by Nexus 3.95 CE (confirmed from the `<fmt>_component` tables in the company DB):

| Format | Nexus | Holiaokho | Notes |
|---|---|---|---|
| Maven 2 | ✅ | ✅ P1 | hosted/proxy/group, tested with mvn |
| npm | ✅ | ✅ P2 | hosted/proxy/group, tested with npm |
| Docker | ✅ | ✅ P3 | including daemon registry-mirror mode, tested with dockerd |
| OCI | ✅ (added in 3.9x, separate from Docker) | ✅ | Same plugin as Docker; referrers API (1.1.0); component detail page lists signatures/SBOMs/attestations (1.1.1); not yet tested with the actual cosign client |
| NuGet (V2 + V3) | ✅ | ✅ V3 (tested with dotnet; V2 not done) | The company has created a repo but isn't using it |
| PyPI | ✅ | ✅ | hosted/proxy/group, tested with pip (PEP 503 HTML index) |
| Raw | ✅ | ✅ | hosted/proxy/group |
| Helm | ✅ | ✅ (tested with helm) | |
| Go | ✅ | ✅ proxy/group (tested with go; no hosted, same as Nexus) | |
| RubyGems | ✅ | ✅ (tested with gem/bundler) | |
| APT | ✅ | ✅ (tested with apt, including GPG signing) | Requires GPG signing |
| YUM | ✅ | ✅ (tested with dnf, including signing) | Requires GPG signing, createrepo logic |
| Conda | ✅ | ✅ (tested with micromamba) | |
| Conan | ✅ | ✅ v2 API (tested with conan) | |
| R (CRAN) | ✅ | ✅ (tested with R) | |
| CocoaPods | ✅ | ✅ CDN proxy (curl) | |
| Cargo | ✅ | ✅ sparse (tested with cargo) | |
| Composer | ✅ | ✅ (tested with composer) | |
| Git LFS | ✅ | ✅ (tested with git-lfs) | |
| p2 (Eclipse) | ✅ | ✅ mirror (curl) | |
| Hugging Face | ✅ | ✅ proxy (tested with huggingface_hub) | |
| Swift | ✅ | ✅ registry (curl) | |
| Alpine (apk) | ✅ | ✅ (tested with apk, RSA signing) | |
| Ansible Galaxy | ✅ | ✅ v3 (tested with ansible-galaxy) | |
| pub (Dart) | ✅ | ✅ (tested with dart) | |
| Terraform | ✅ | ✅ provider mirror + module registry (tested with terraform) | |

All 25 formats are implemented (2026-09-13), each tested with its official client in a Docker container; see `scripts/e2e-formats.sh`.

## 2. Repository Types and Behavior

| Feature | Nexus | Holiaokho |
|---|---|---|
| hosted / proxy / group | ✅ | ✅ Generic at the engine layer |
| Proxy: contentMaxAge / metadataMaxAge | ✅ | ✅ |
| Proxy: negative cache + TTL | ✅ | ✅ |
| Proxy: upstream authentication (Basic / Bearer / preemptive) | ✅ | ✅ Basic (sent preemptively), registry Bearer token flow |
| Proxy: custom CA / trust store, outbound HTTP proxy, connection timeout, retries, autoBlock | ✅ | ✅ Custom CA, outbound HTTP proxy (global), connection timeout, autoBlock; **automatic retry** (1.4.0): GET/HEAD requests retry on connection errors, timeouts, and 429/502/503/504, defaulting to 2 retries (`proxy.retries`, overridable per repo), honoring `Retry-After` (capped at 5 seconds). A stalled Docker layer streaming download resumes from where it left off (1.1.2) |
| Proxy: blocked (suspend upstream) | ✅ | ✅ (including autoBlock, stale-if-error) |
| Group: member ordering, first-match, metadata merge | ✅ | ✅ (since 1.4.0 the member list can be reordered by drag-and-drop or arrows; new members are added last) |
| Group: deploy to a group (forwarded to the first hosted member) | ✅ | ✅ 1.4.0: all formats (tested with the official clients for Maven, npm, PyPI, Docker, etc.); requires write permission on both the group and that hosted repo; npm login/audit and Git LFS batch requests are still handled by the group directly |
| Hosted: writePolicy (ALLOW / ALLOW_ONCE / DENY) | ✅ | ✅ |
| Maven: layoutPolicy STRICT/PERMISSIVE, versionPolicy RELEASE/SNAPSHOT/MIXED, contentDisposition | ✅ | ✅ (contentDisposition not done) |
| Maven: timestamped snapshot versions, `maven-metadata.xml` generation and merging, checksum sidecar files | ✅ | ✅ |
| Maven: rebuild metadata task, maven-indexer | ✅ | ✅ rebuild metadata (`rebuild-indexes` task) / maven-indexer Later |
| Docker: httpPort / httpsPort connector, subdomain connector, path mode | ✅ | ✅ All of them |
| Docker: v1 API | ✅ (off by default) | Won't do (deprecated by Docker itself) |
| Docker: forceBasicAuth, Bearer token realm | ✅ | ✅ |
| Docker: foreign layer caching, index type (HUB / registry / custom) | ✅ | ✅ index type; foreign layers are fetched directly by the client |
| Docker: group supports push (Pro) | Pro | ✅ 1.4.0 (pushing to a group stores into the first hosted member) |
| Routing Rules (allow / block regex) | ✅ | ✅ |
| Content Selectors (CSEL expressions, paired with privileges) | ✅ | ✅ A subset of CSEL (==, !=, =^, =~, and/or/not) |
| Strict content type validation | ✅ | Later (not done) |
| Online/offline toggle | ✅ | ✅ |
| Attaching Cleanup Policies at the repository level | ✅ | ✅ |

## 3. Storage

| Feature | Nexus | Holiaokho |
|---|---|---|
| File blob store | ✅ | ✅ |
| S3 blob store (including IAM role, encryption, prefix) | ✅ | ✅ (tested with MinIO; IAM role goes through the SDK's default credential chain) |
| Azure Blob store | ✅ | Later |
| Google Cloud Storage | ✅ | Later |
| Group blob store (aggregating multiple blob stores) | Pro | Later |
| Blob store soft-delete + hard-delete task | ✅ | ✅ blob-gc (soft) + compact-blobs |
| Blob store quota and alerts | ✅ | ✅ |
| Blob store capacity/metrics | ✅ | ✅ |
| Cross-repo deduplication (content-addressable) | ❌ (Nexus doesn't dedupe) | ✅ Built in, **beyond Nexus** (Docker layers aren't re-fetched across repos) |
| Change blob store (moving a repo to a different store) | ✅ | Later |

## 4. Security and Permissions

| Feature | Nexus | Holiaokho |
|---|---|---|
| Local users (bcrypt/Shiro) | ✅ | ✅ argon2id, with automatic upgrade after Shiro1 verification |
| Roles/privileges (repo-view, repo-admin, application, wildcard, script) | ✅ | ✅ (script not done) |
| Anonymous access toggle + anonymous role | ✅ | ✅ |
| LDAP / Active Directory | ✅ | ✅ (tested with OpenLDAP; AD configured via memberOf) |
| SAML | Pro | Later (OIDC is already done; most IdPs support both) |
| OIDC | ❌ (Nexus doesn't have this) | ✅ **Beyond Nexus** (tested with Dex) |
| User tokens (long-lived tokens for CI) | Pro | ✅ **Beyond Nexus CE** |
| Docker Bearer token realm | ✅ | ✅ |
| npm Bearer token realm (`npm login`) | ✅ | ✅ |
| NuGet API key realm | ✅ | ✅ (X-NuGet-ApiKey = user token) |
| Conan / Hugging Face / Pub / Terraform / Ansible Galaxy token realm | ✅ | ✅ Each format accepts a user token |
| OCI Bearer token realm (separate from Docker) | ✅ | ✅ `/v2/token` (shared by Docker and OCI) |
| Login failure rate-limiting (returns 429 after repeated failures) | ✅ | ✅ (default 10 per minute, per IP) |
| Secret encryption key (`nexus.secrets.file`), key rotation and re-encryption task | ✅ | ✅ AES-256-GCM (`secrets.key`/key_file), `previous_keys` + `re-encrypt-secrets` task |
| Privilege types: wildcard / application / repository-admin / repository-view / script | ✅ (365 by default) | ✅ target/actions model |
| Rut Auth (reverse-proxy header authentication) | ✅ | ✅ |
| Default Role realm | ✅ | ✅ |
| Realm activation order | ✅ | ✅ (local/ldap order) |
| SSL trust store (importing upstream certificates) | ✅ | ✅ `proxy.ca_cert_file` |
| Binding privileges to a content selector | ✅ | ✅ |
| Password policy, account lockout | Partial | ✅ Login rate-limiting (per IP) + password complexity policy (length, character classes, must not contain the username, common-password check; `PUT /auth/settings`) |
| Audit log | ✅ (capability) | ✅ Basic (admin operations) |

## 5. Maintenance Tasks

Nexus's built-in scheduled tasks:

| Task | Nexus | Holiaokho |
|---|---|---|
| Cleanup Policies execution (by lastDownloaded / lastBlobUpdated / regex / keep N versions) | ✅ | ✅ Basic |
| Cleanup preview (dry-run) | ✅ | ✅ |
| Admin - Compact blob store (hard-delete soft-deleted blobs) | ✅ | ✅ compact-blobs |
| Admin - Delete blob store temporary files | ✅ | ✅ `delete-temp-files` task (1.1.3): filesystem temp files, leftover S3 `uploads/` objects and multipart uploads started more than 7 days ago and still incomplete, proxy download temp files; only clears files untouched for over 1 hour |
| Admin - Export databases for backup | ✅ | ✅ backup task + `holiaokho backup/restore` + API |
| Admin - Log database table record counts | ✅ | Won't do (replaced by metrics) |
| Admin - Remove a member from a blob store group | Pro | Later |
| Repair - Rebuild repository browse | ✅ | Not needed (browse queries the DB directly) |
| Repair - Rebuild repository search | ✅ | Not needed (search queries the DB directly) |
| Repair - Reconcile component database from blob store | ✅ | Won't do: the DB is the single source of truth, relying on backup/restore instead; blobs are content-addressed and carry no metadata |
| Repair - Rebuild Maven repository metadata | ✅ | ✅ (automatic after deleting a component; no separate task) |
| Repair - Reconcile npm /-/v1/search metadata | ✅ | Not needed (search queries the DB directly) |
| Repair - Rebuild Yum/APT metadata | ✅ | ✅ rebuild-indexes task (Maven/APT/YUM/apk/CRAN/Conda) |
| Docker - Delete unused manifests and images | ✅ | ✅ Covered by Cleanup Policies + blob GC |
| Docker - ECR token refresh (automatic token renewal when proxying AWS ECR) | ✅ | Later |
| Cleanup unused `<format>` blobs (one per format, scheduled automatically out of the box) | ✅ | ✅ Handled uniformly by blob GC |
| System - Repository Health Check (RHC, connects to Sonatype vulnerability data) | ✅ (connects to IQ) | Won't do (same as IQ) |
| Malicious Risk on Disk auto-enabling RHC | ✅ (3.7x+) | Won't do (same as IQ) |
| Docker - Delete incomplete uploads | ✅ | ✅ `delete-incomplete-uploads` task (1.1.3): incomplete uploads untouched for 24 hours (supported on both filesystem and S3) |
| Maven - Delete SNAPSHOT, Delete unused SNAPSHOT, Remove snapshots from group, Purge unused | ✅ | ✅ Expressed through Cleanup Policies (prerelease=true / regex / lastDownloaded) |
| Maven - Publish Maven Indexer files | ✅ | Later |
| Statistics - Recalculate vulnerabilities | IQ | Won't do |
| Task: cron scheduling, manual trigger, run history, email notifications | ✅ | ✅ |
| Task: distributed locking across multiple nodes | Pro | Later (together with HA) |

## 6. Administration and System

| Feature | Nexus | Holiaokho |
|---|---|---|
| Web UI: browse (tree view), search, upload, repo/user/role/task management | ✅ | ✅ |
| Search (by format / group / name / version / checksum / custom attributes) | ✅ | Partial: format, repository, group (namespace), name, version, keyword; checksum and custom attributes not done (Later) |
| REST API (`/service/rest/v1`) | ✅ | ✅ Our own `/api/v1` + a Nexus-compatible subset (status, repositories, search, components, assets, upload) |
| OpenAPI / Swagger UI | ✅ | Partial: OpenAPI spec (`/api/v1/openapi.yaml`); Swagger UI not provided (Later) |
| Webhooks (repository / audit / global events) | ✅ | ✅ HMAC signing, event filtering |
| Email server configuration | ✅ | ✅ |
| HTTP settings (user agent, timeout, outbound proxy, non-proxy hosts) | ✅ | ✅ Config file |
| System status / health check | ✅ | ✅ `/healthz` `/readyz` `/api/v1/status/check` |
| Health check details (`status/check`) | ✅ | ✅ DB, storage, quota, default password, scheduler |
| `http.forwarded` capability (trusting X-Forwarded-* headers) | ✅ | ✅ |
| UI settings: session timeout, etc. | ✅ (`rapture.settings`) | ✅ session_ttl (config file); the rest is frontend-only |
| List of formats supported by UI upload (18: apt, maven2, raw, alpine, ansiblegalaxy, composer, conda, go, helm, npm, nuget, pypi, pub, r, rubygems, swift, terraform, yum) | ✅ (`formats/upload-specs`) | Each plugin declares its own upload spec, and the UI generates the form from it |
| Blob store: softQuota, blobCount, totalSize, availableSpace reporting | ✅ | ✅ |
| Node identity (multi-node identification) | ✅ | Later (with HA) |
| Metrics (Prometheus) | ✅ (`/service/metrics/prometheus`) | ✅ `/metrics` (basic metrics) |
| Logging: view logs in the UI, adjust log level dynamically | ✅ | ✅ `/system/logs`, `/system/log-level` |
| Support zip | ✅ | ✅ `/system/support-zip` |
| Logs: view log file contents directly in the UI | ✅ | ✅ |
| System Information page | ✅ | ✅ `/system/info` |
| Recovery Mode (safe mode to repair data consistency) | ✅ (3.9x) | Later |
| Data Store settings page (shows DB connection) | ✅ | ✅ `/system/config` (password masked) |
| Proprietary Repositories (marking which hosted repos hold in-house components, for IQ's namespace-confusion protection) | ✅ | Won't do (IQ-specific) |
| Nodes page (node list) | ✅ | Later (with HA) |
| Licensing page | ✅ | Won't do (no concept of a Pro license) |
| Nexus One UI toggle | ✅ (new UI in 3.9x) | Won't do |
| UI search bar searches both components and CVEs | ✅ | Searches components only |
| Repositories list: Size, Health Check, Firewall Report columns | ✅ | Size is there; the other two won't do (IQ) |
| Capabilities system (feature toggles) | ✅ | Replaced by the config file |
| Scripting API (Groovy) | ✅ (off by default) | Won't do (replaced by REST + CLI) |
| Backup/restore | ✅ (H2 export) | ✅ tar.gz including blobs, via CLI and API |
| Nexus upgrade/migration tool | ✅ | ⏸ Implemented (`import-nexus`) but disabled by default, requires `HOLIAOKHO_ENABLE_NEXUS_IMPORT=1`; not exposed externally for now |
| Branding, Outreach, Analytics upload | ✅ | Won't do |
| Malware remediation / Repository Firewall / RHC | IQ / Pro | Not integrating with Sonatype IQ; instead we built our own **vulnerability scanning** (1.2.0) and **malicious package blocking** (1.4.0, rejecting downloads based on the OSV/OpenSSF malicious-packages list, see §8) |

## 7. Features Available Only in Paid Nexus Editions

> The Nexus licensing terms in the table below change according to Sonatype's announcements; refer to their official latest terms for accuracy.
> This is recorded here only as a planning reference, not as a basis for commercial comparison.

| Feature | Plan |
|---|---|
| CE limits: 100,000 components, 200,000 requests/day | ✅ **No limit** |
| User tokens | ✅ |
| SAML | Later |
| HA / clustering | Later (the architecture doesn't block it; see the non-goals in §1) |
| Staging/tagging/promotion | Later |
| Repository replication | Later |
| Group blob store | Later |
| Docker group push | ✅ 1.4.0 (all formats can now deploy to a group, see §2) |
| Content replication, Import/Export | Later |

---

## 8. Features Added Beyond the Parity Table

> Features not built to match Nexus. This section makes no claim about whether Nexus has them or not — it simply records what we built.

| Feature | Status | Version |
|---|---|---|
| Forced password change on first login: the bootstrap admin and any account whose password was reset by an admin can only call the change-password API until they do | ✅ | 1.0.0 |
| OCI referrers API: signatures/SBOMs/attestations produced by cosign and syft can be stored alongside an image and queried back; proxies query upstream and cache the result | ✅ | 1.1.0 |
| Component detail page lists which signatures/SBOMs/attestations are attached to a Docker image | ✅ | 1.1.1 |
| Streaming proxy downloads (Docker layers): multiple clients share the same upstream download | ✅ | 1.1.2 |
| No overall time limit for slow downloads: a download is only considered stalled after 1 minute with no data, and resumes from where it left off using `Range` | ✅ | 1.1.2 |
| An upstream rejection (e.g., ghcr returning 403 for a nonexistent image) no longer triggers autoBlock; when blocked, the response is "upstream unavailable" rather than "not found" | ✅ | 1.1.2 |
| Vulnerability scanning: matches stored components against OSV.dev (Maven, npm, PyPI, Go, NuGet, RubyGems, Cargo, Composer, pub, CRAN), merges aliases into a single entry, sorts by severity, and lists the fixed version; scans everything by default, can be turned off per repository; newly found Critical/High vulnerabilities trigger a one-time email and webhook notification; shown on the dashboard; formats OSV doesn't support are labeled "not covered" rather than "safe" | ✅ | 1.2.0 |
| Vulnerability report export: PDF, Excel (follows the UI language, with a subsetted Noto font embedded), CSV, JSON (includes purl, for programs and AI agents to match against dependencies); each component lists the upgrade version that fixes all known vulnerabilities | ✅ | 1.3.1 |
| New release notification (checks GitHub daily, can be disabled) | ✅ | 1.3.0 |
| Vulnerability reports let you choose which severities to export; excluded severities are labeled "not included in this report" rather than shown as 0 | ✅ | 1.3.2 |
| Vulnerability data requires login to read, even when anonymous access is enabled | ✅ | 1.3.3 |
| Vulnerability lists and reports show each component's "last used" time (last download, or the storage time if never downloaded) | ✅ | 1.3.4 |
| Every table site-wide can be sorted by clicking its column headers; paginated lists (search, components, vulnerabilities) are sorted server-side, with version numbers sorted by version order | ✅ | 1.3.5 |
| Cron preview is computed server-side and labeled with the server's time zone, showing both the user's local time and the server time | ✅ | 1.3.5 |
| Malicious package blocking: packages OSV flags as malicious (MAL-, CWE-506) are rejected on download (for both cached and proxied content); blocks are logged, trigger email/webhook notifications, and an admin can allow one through with a stated reason; reports mark it "please remove" | ✅ | 1.4.0 |

---

## How to Use This

- Check this table before adding a feature; update the status as each item is completed
- If you find a Nexus feature missing from this table → add it first, then decide on a stage
- Every "Won't do" item must have a reason, and it must be possible for AndyShiu to overrule it

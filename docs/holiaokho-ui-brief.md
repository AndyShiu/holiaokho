English | [繁體中文](holiaokho-ui-brief.zh-TW.md)

# Holiaokho Web UI — Design Brief

> A complete brief for Claude Design: what this product is, who it's for, what pages it has, each page's data and actions, the key flows, the direction for visual style, and the technical constraints.
> Reading this document should be enough to produce the design system and the layout for every page, without needing to ask the backend team anything.
> Section 5 is the business logic shared across the whole site; Section 6 goes page by page through components, interactions, and front-end business logic (field names all match the real API).
> The backend API is already finished; the spec lives at `/api/v1/openapi.yaml` (32 paths). This document describes things from the user's point of view and does not repeat the API details.
> 2026-09-13.

---

## 1. What the product is

**Holiaokho** (Taiwanese Hokkien "好料庫" hó-liāu-khòo; English tagline: "the good-stuff store", pronounced ho-LIAO-kho) is a **self-hosted artifact repository manager**: the place a development team stores, caches, and distributes every kind of package — Java, JavaScript, Docker images, Python, .NET, Go, Linux packages, and more, 25 formats in all.

It is a **drop-in replacement for Sonatype Nexus Repository**: the same URL structure, the same repository concepts, but a single Go binary, low memory use, and none of the OSS edition's cuts (no limit on the number of components, no limit on daily requests, with OIDC and user tokens).

One-line positioning: **"The team's internal package store — install it and swap out Nexus, painlessly."**

### Core concepts (used throughout the UI)

| Concept | Description | UI term |
|---|---|---|
| **Repository** | A named package store, belonging to one **format** (Maven, npm, Docker…) and one **type** | Repository |
| **type: hosted** | Content the team uploads itself | Hosted |
| **type: proxy** | A cache of an upstream (Maven Central, the npm registry, Docker Hub…); once downloaded, an artifact stays local | Proxy |
| **type: group** | Combines several hosted + proxy repositories into one URL, so a client only needs to configure a single address | Group |
| **Package** | A logical package version (Maven's groupId:artifactId:version, npm's name@version, Docker's image:tag) | Package (or "Component", depending on the format's display) |
| **Asset** | The actual file (.jar, .tgz, manifest, layer) | Asset / File |
| **Blob** | The content-addressed physical bytes, deduplicated across repositories (users normally never need to see this) | — |
| **Storage** | Where blobs live: local disk or S3-compatible storage | Storage |
| **Format** | The package ecosystem's protocol | Format |

**25 formats**: Maven, npm, Docker/OCI, PyPI, raw, NuGet, Helm, Go, APT, YUM, Alpine, RubyGems, Cargo, Composer, Conda, R, p2, CocoaPods, Terraform, pub (Dart), Git LFS, Hugging Face, Ansible Galaxy, Conan, Swift. Each one needs its own icon (using that ecosystem's conventional colour or an abbreviation is fine; using any ecosystem's actual trademarked logo is not).

---

## 2. Users

| Role | Who | Frequency | Main tasks |
|---|---|---|---|
| **Admin / DevOps** (primary design target) | The people who set up and maintain the server (1–3 people) | Daily during setup, occasional afterwards | Create repositories, set permissions, check health, free up space, handle CI complaints |
| **Developer** (secondary) | Engineers on the team (10–100 people) | Occasional | Find packages, check versions, copy "how to configure the client" snippets, create their own tokens |
| **CI bot** | GitHub Actions / GitLab CI / Jenkins | Thousands of times a day | **Never uses the UI** — only hits repository URLs and the API |
| **Auditor / manager** | Occasional viewer | Rare | Who uploaded what, storage-usage trends |

Design priority: **the admin's efficiency and sense of trust come first**; developer-facing pages need to work "browsable without logging in (if anonymous access is on), find a package within three seconds, copy the setup with one click."

---

## 3. Brand and style direction

- **Where the name comes from**: Taiwanese Hokkien "好料" (hó-liāu) means the good stuff — the real, well-sourced ingredients; "庫" (khòo) means a storehouse. The brand should carry **Taiwanese/East-Asian confidence and warmth**, while the interface itself is an **internationalised developer tool** (five languages).
- **Tone**: practical, fast, trustworthy, with a bit of warmth. Not a flashy consumer product, and not a cold enterprise back office.
- **Reference feel**: Grafana's information density, Linear's polish, GitHub's readability. **Avoid** looking like Nexus (Sonatype's dark-grey-and-green style) or JFrog (green).
- **Colour**: undecided — please propose. Feel free to draw on "好料" for inspiration (a warm amber/orange as the accent colour, paired with a calm dark blue-grey as the primary colour, for example), but it must work in both the light and dark themes, and must not clash with the colours of the format icons.
- **Logo / wordmark**: needs a proposal. The wordmark text is "Holiaokho", which can pair with a simple mark suggesting a storehouse/grid/store; the Chinese "好料庫" serves as a subtitle. The homepage and README will use the phrasing `Holiaokho (hó-liāu-khòo, "the good-stuff store")`.
- **Typeface**: system fonts for the interface (with CJK in mind: Noto Sans TC/SC/JP/KR, or the system default); code, paths, and commands always set in monospace.
- **Component library constraint**: the front end is built with **React + TypeScript + Ant Design**. Base the design on AntD's component vocabulary (Table, Form, Drawer, Modal, Tree, Tabs, Tag, Steps…), with custom design tokens (colour, corner radius, spacing, type scale). Avoid designing interactions that AntD cannot deliver.

---

## 4. Site-wide structure (information architecture)

```
/ui
├── Login (local username/password, LDAP, or a "Log in with SSO" button, shown depending on server config)
├── Dashboard
├── Browse (browse all repositories and their content)        ← Developer's main entry point
├── Search (search packages across all repositories)
├── Vulnerabilities (findings and export; blocked malicious packages)
├── Repositories (management)
│   ├── List
│   ├── Creation wizard (choose format → choose type → configure)
│   └── Single repository: Settings / Content / Usage / Stats
├── Storages
├── Security
│   ├── Users
│   ├── Roles (including privilege editing)
│   ├── Content Selectors
│   ├── Auth Settings (realm order, LDAP, OIDC, Rut Auth, default roles, anonymous access)
│   └── My Tokens (available to every logged-in user; can also live in the personal menu, top right)
├── Maintenance
│   ├── Tasks (schedule and run history)
│   ├── Cleanup Policies (with preview)
│   ├── Routing Rules (with test)
│   └── Backup / Restore
├── Integrations
│   ├── Webhooks
│   └── Email
└── System
    ├── Health / Status
    ├── System Information
    ├── Logs (live tail + log level)
    ├── Configuration (read-only, secrets masked)
    ├── Support ZIP
    └── Audit Log
```

A fixed navigation on the left (collapsible to icons); along the top: global search, language switcher, theme switcher, notifications (task failures / quota warnings), and a personal menu (Tokens, change password, log out).

**Permissions affect what's shown**: an anonymous visitor or an ordinary developer sees only Browse / Search / My Tokens; an admin sees everything. Items without permission are **not shown**, rather than shown disabled.

---

## 5. Business logic shared across the whole site (every page depends on this)

Both design and front-end work should start from this section; each page below only covers what's specific to it.

### 5.0.1 Startup and identity
1. On load, the app first calls `GET /api/v1/auth/methods` (public) → gets `{local, ldap, oidc, oidcLoginUrl, anonymous, passwordPolicy}`, stored as the global `authMethods`.
2. Then it calls `GET /api/v1/session` (whoami) → `{username, roles[], anonymous, via, privileges[]}`.
   - `anonymous: true` and `authMethods.anonymous: true` → render as an anonymous user (can see Browse / Search).
   - `anonymous: true` and `authMethods.anonymous: false` → redirect to the login page, remembering the original path as `next`.
   - 401 → same as above.
3. `privileges[]` is a list of `{target, actions[]}`; the front end uses it to decide **what shows up in navigation and which buttons appear** (not disabled — simply absent). The check function:
   - `can(target, action)`: true if the target matches exactly, or the privilege target is `*`, or it's a matching wildcard prefix (`app:*`, `repo:*`, `format:*`); and the actions include that action or `*`.
   - Navigation mapping: Repositories management → `app:repositories read`; Storages → `app:storages read`; Users → `app:users read`; Roles / Content Selectors → `app:roles read`; Tasks → `app:tasks read`; Auth Settings / Webhooks / Email / System / Audit → `app:system read`; Search → `app:search read`; the Dashboard health cards → `app:status read`.
   - Repository level: the upload / delete buttons → `repo:<name> write/delete` (or `format:<format>`, or `*`).
4. After login, whoami is fetched again; after logging out (`DELETE /session`), state is cleared and the user is returned to the login page.
5. Session timeout: any API returning 401 → a global interceptor pops up "Your session has expired" → redirects to login with `next`; this should **not** pop up on pages that are readable anonymously (a 401 while anonymous just means this particular action needs login — show a "Log in to continue" button instead).

### 5.0.2 Error handling
- Backend errors have a fixed shape, `{code, message, params?}`, with HTTP status codes: 400 validation, 401 not logged in, 403 forbidden, 404, 409 conflict (duplicate name, version already exists), 429 rate-limited, 502 upstream failure, 507 quota exceeded.
- Front-end translation order: try `errors.<code>` (with params) first; if there's no translation, show the raw `message`, with the code shown in small text alongside it (to make reporting the issue easier).
- Form-related errors (400 / 409) show in an Alert at the top of the form and keep the user's input; list-action errors use a toast.
- 502 upstream errors include a "View upstream settings" link (for proxy repositories).

### 5.0.3 Common list behaviour
- The backend currently **returns the whole array** for repositories, users, roles, storages, tasks, policies, etc. — none of these are paginated. Only `search`, `packages`, `audit`, and `logs` take `limit`/`offset` or `tail`. The front end does local sorting/filtering/pagination on the full arrays (AntD Table's built-ins); `search` and `packages` use server-side pagination (50 per page, `offset` accumulating).
- Every list has: a toolbar at the top (a search box debounced at 300ms, filters, the primary action button on the right), sortable columns, a click on a row to open its detail, and a `⋯` action menu at the end of each row.
- Deletion always goes through a confirmation modal; "high-risk" actions (repository, storage, user, restoring a backup, running a cleanup) require **typing the name** before the confirm button is enabled.

### 5.0.4 Common form behaviour
- Use a Drawer (on the right, 560–720 wide) for create/edit, with the list staying visible behind it; multi-step wizards (creating a repository) use a full page instead.
- Warn before leaving a dirty form.
- On successful save: toast + close the Drawer + refetch the list. On failure: keep the Alert inside the Drawer.
- Secret fields (passwords, secretKey, token secrets, signing private keys): the backend always returns `***` when read back. Front-end rule: **leaving the field empty, or leaving it as `***`, means "unchanged"** — only a newly typed value gets submitted. UI: a password input with a placeholder like "Already set — leave empty to keep it."

### 5.0.5 Time, size, and copying
- Time columns show relative time ("3 minutes ago"), with the absolute ISO time on hover; both follow the current locale.
- Bytes display as KB/MB/GB (base 1024); quota inputs are in GB, multiplied by 1024³ into bytes on submit.
- The `<Copyable>` component: monospace text, a copy icon at the end, the icon turns into a checkmark for 1.5 seconds after clicking; long strings (sha256, tokens) are truncated in the middle, with the full text shown on hover.
- "Usage" code snippets: syntax-highlighted, a copy button top right, with the base URL filled in from `window.location.origin` (or from the backend's `status.baseUrl`, if set, which takes priority).

### 5.0.6 Polling
- No WebSocket. The Dashboard health cards poll every 30s; the Tasks list polls every 3s while any task has `running: true`; Logs poll every 2s when live updates are on; no other page polls. Polling pauses when the page is hidden (`document.hidden`).

### 5.0.7 Localisation
- The language is stored in localStorage; the initial value maps from the browser's language (zh-TW / zh-CN / ja / ko / en, anything else → en).
- Every piece of copy is keyed; the backend error-code lookup table lives under `errors.*`; format names, type names, and task names all need translations too.

---

## 6. Detailed page specifications

Each page follows this shape: **route / permissions** → **layout and components** → **data sources** → **interactions** → **business logic** → **states**.

### 6.1 Login `/ui/login`

**Permissions**: public. A logged-in user landing here is redirected straight to `next` or the Dashboard.

**Layout and components**
- A centred card (400 wide): logo + wordmark, subtitle "好料庫".
- Form: username, password (with a show/hide toggle), a "Log in" button (with a loading state).
- If `authMethods.oidc` is true → a divider below the form plus a "Log in with SSO" button.
- If `authMethods.anonymous` is true → a link at the bottom, "Browse packages without logging in →", to Browse.
- The language switcher sits top right (must work even before login).

**Data source**: `POST /api/v1/session {username, password}` → 200 sets a cookie; `GET /api/v1/auth/oidc/login?next=<path>` does a full-page redirect.

**Interactions and business logic**
- Enter submits; the form is disabled while submitting.
- 401 → an Alert at the top of the form, "Incorrect username or password" (without saying which one is wrong); 429 → an Alert, "Too many failed login attempts, try again later", and the button is locked with a 30-second countdown.
- On success: refetch whoami → if `can("app:status","read")`, also call `GET /status/check`; if `checks.default_admin_password.healthy === false`, redirect to the **forced password change** page (6.2); otherwise redirect to `next` (only relative, in-site paths are accepted, to prevent open redirects) or to the Dashboard.
- When returning from OIDC, the callback has already been handled by the backend and the user is redirected to `next`; the front end only needs to refetch whoami on load.
- Never remember the password, and there's no "forgot password" flow (an administrator resets it instead).

**States**: no empty state; if the backend is unreachable, show "Cannot reach the server" with a retry.

### 6.2 Forced password change (first login) `/ui/change-password?forced=1`

**Layout**: a full-page centred card, no sidebar. Title "Set your own password first," with one line of explanation.
- Fields: current password, new password, confirm new password.
- **Password-rules list component `<PasswordRules>`**: dynamically lists rules from `authMethods.passwordPolicy` (minimum N characters, needs an uppercase letter, a lowercase letter, a digit, a symbol (if required), must not contain the username, must not be a common password); each rule ticks or crosses off live as the user types. The "not a common password" rule can't be checked on the front end, so it shows a neutral icon noting "checked on submit."
- The submit button stays disabled until every rule the front end can check has passed and both entries match.

**Data source**: `PUT /api/v1/me/password {current, password}` → 204.

**Business logic**
- 401 (wrong current password) → an error on the current-password field; 400 `password.*` → the matching rule row turns red with its message (e.g. `password.common`).
- On success, refetch whoami and go to the Dashboard; the same component, without `forced`, is also used for "Change password" in the personal menu (inside a Drawer).

### 6.3 Dashboard `/ui/`

**Permissions**: any logged-in user; cards show based on permissions (no health cards without `app:status`; developers see a stripped-down view: their token count, recently browsed repositories, a quick search).

**Layout and components (Admin)**
1. First row, **health cards**: one small card per check (icon + name + status colour): `database`, `storage:<name>` (a progress bar of usedBytes/quotaBytes — yellow above 90%, red once exceeded), `scheduler`, `default_admin_password` (the whole card turns red when unhealthy, with a "Change now" button). A single overall status badge at the top: green for `healthy`, red if there's a problem.
2. Second row, **stat cards**: number of repositories (broken down by type — hosted/proxy/group), total packages, total assets, total size (the sum of every repository's `stats.size`).
3. Third row, left: **recent activity** (the last 10 audit entries: relative time, actor, translated action, a link to the target); right: **tasks** (tasks with `lastStatus === "failed"` are listed first and highlighted in red; the rest show their nextRun; each row has "Run now").
4. Top right: **quick action** — "Create Repository".

**Data source**: `GET /status/check` (polled every 30s), `GET /repositories` (including stats), `GET /audit?limit=10`, `GET /tasks`.

**Business logic**
- Detecting a brand-new install: `repositories.length === 0` → replace the stat cards with a **getting-started panel**: three Steps ("Change the admin password" ✓, driven by `default_admin_password`; "Create your first proxy" → opens the wizard with maven/proxy pre-selected; "Copy the client setup" → goes to that repository's Usage tab).
- If any single card fails to load, only that card is affected (it shows a retry) — the whole page never errors out.

### 6.4 Browse `/ui/browse/:repo?/:path*`

**Permissions**: `app:repositories read` is not required; the list comes from `GET /repositories` (the backend already filters by each repository's read permission). Anonymous access is allowed.

**Layout and components**
- Left column (280 wide, resizable): the **repository list**
  - Top: a search box (filters names locally), filter chips (multi-select format, multi-select type).
  - Each row: format icon, name, a type tag (Hosted in blue / Proxy in purple / Group in green — colours to be designed), size in grey on the right; `online: false` shows the row greyed out with a small "Offline" tag.
  - The selected row is highlighted, and the URL stays in sync at `/ui/browse/<name>`.
- Right column:
  - **Header row**: repository name, format/type tags, a copyable repository URL (the `url` field), and buttons on the right: "Usage" (opens a Drawer), "Upload" (hosted repositories with write access), "Refresh."
  - **Tabs**: Content, Packages.
  - **Content tab**: breadcrumbs (click to go up) plus a **file table**: name (folders have a clickable icon; files have their own icon), size, updated time, `cacheExpiresAt` (proxy only — shows "Cache expires" as a relative time; grey once expired), actions (download, details, delete). Folders come before files, each sorted by name.
  - **Packages tab**: a table of namespace / name / version / last downloaded / created, with server-side pagination; a name/namespace filter box at the top; clicking a row opens the Package Drawer (see 6.5).
  - Proxy repositories show a light banner at the top of the content table: "Only cached content is listed here — this is not the full upstream catalogue."
  - Group repositories show: "Merged from N members: a, b, c" — each member name links through.

**File detail Drawer** (`GET /assets/{id}`): path (copyable), size, contentType, `blobDigest` (copyable), the package it belongs to (linked), created/updated/last-downloaded times, cache expiry, a "Negative cache (an upstream 404 record)" label when `negative: true`, a download URL (copyable, `<repoUrl>/<path>`), a "Delete" button (`repo:<name> delete`).

**Upload Drawer** (hosted only; `POST /repositories/{name}/upload`, multipart `file`, `path`, `overwrite`): a drop zone, a path input (defaulting to the current browsing directory plus the filename, editable), an "overwrite if the path exists" checkbox (when writePolicy is `allow_once`, a note explains "This repository does not allow overwriting — checking this has no effect"), a progress bar. A 409 `repo.redeploy_denied` shows a message that the version already exists.

**Data source**: `GET /repositories`, `GET /repositories/{name}/browse?path=`, `GET /repositories/{name}/packages?q&name&namespace&limit&offset`, `GET /assets/{id}`, `DELETE /assets/{id}`.

**Business logic**
- The URL is the single source of truth: reloading `/ui/browse/maven-central/com/google/gson` must land back in the same place.
- Entering a folder shows a skeleton first, then swaps in the response; going back up uses a cache (the most recent 20 directories are kept).
- "Download" opens `<repoUrl>/<path>` directly (a browser GET, relying on the cookie or on anonymous access).
- After a delete, the row disappears from the table with a toast; if the deleted asset was a package's last one, the backend deletes the package too, so the Packages tab needs refetching.
- Docker repositories' content paths are `v2/<image>/manifests/<tag>` and `blobs/sha256:…`; for the Docker format, the front end displays files under the `manifests` directory as a "tag", and files under `blobs` as a short digest; the Packages tab shows Docker packages as `image:tag`.

**States**: no repositories → an empty state, "No repositories yet" (admins see a create button); an empty repository → "This repository has no content yet" (proxy adds "Content appears here after the first download"; hosted adds an upload button); an offline repository → its content can still be browsed as normal, but the header row shows a yellow Alert, "This repository is offline: client requests are rejected."

### 6.5 Search `/ui/search?q=&format=&repository=&namespace=`

**Permissions**: `app:search read` (the anonymous role has it by default).

**Layout and components**
- A large search box at the top (autofocus, triggered by Enter or a 400ms debounce) plus a filter row: format (a Select with icons), repository (a Select, linked to the chosen format), namespace (an input), version (an input).
- Results table: format icon, repository (links to Browse), namespace, name, version, last downloaded, created; 50 per page, with "Load more" at the bottom (accumulating `offset`) or pagination.
- Clicking a row opens the **Package Drawer**: titled `namespace/name@version`, its repository, attributes (`attrs`, shown per format: npm's description/license, Maven's packaging, Docker's digest/size, etc.; anything else as a key-value table), an **Assets table** (path, size, sha256, download), a "Delete this version" button top right (`repo:<name> delete`, with a confirm modal).
- The global search box in the top bar: pressing Enter jumps straight to this page with `q` set.

**Data source**: `GET /search?q&format&repository&namespace&name&version&limit&offset`, `GET /packages/{id}`, `GET /packages/{id}/assets`, `DELETE /packages/{id}`.

**Business logic**
- `q` does a substring match against name/namespace (server-side); if the input contains `@` (an npm scope) or `:` (a Maven GAV), the front end doesn't parse it specially — it's sent as-is.
- Any filter change re-queries and resets the offset; every query parameter stays synced to the URL.
- With no criteria at all, no query runs — show an empty state with example keywords (`gson`, `@babel/core`, `library/alpine`).
- Deleting a version removes it from the results.

### 6.5.1 Vulnerabilities `/ui/vulnerabilities?tab=&level=&severity=&format=&repository=&q=`

**Permissions**: signed in (anonymous callers always get 401, even with anonymous access on) plus `app:search read`; only findings in repositories the user can read are listed. Allowing a malicious package takes `app:system write`.

**Layout**: a Segmented control at the top with two tabs — "Vulnerabilities" and "Blocked malicious packages (N)"; `tab=blocked` is kept in the URL.

**Vulnerabilities tab**
- Four severity cards (Critical / High / Moderate / Low; a click filters to that level) and a coverage line (checked, waiting, not covered by OSV — which is not the same as safe, scanning off).
- Filter bar: keyword (package, GHSA or CVE id), minimum severity, format, repository.
- Results table (sorted by the server, 50 per page): severity (malicious packages get an extra red "Malicious" tag), vulnerability id (links to osv.dev, with its CVE alias), package, repository, summary, fixed in (malicious packages say "Remove it — no version is safe"), last used, found. Clicking a row opens the Package Drawer.
- Top right: "Export report" — choose the format (PDF / Excel / CSV / JSON) and the severities to include; the PDF follows the interface language. "Scan now" (`app:tasks write`).

**Blocked malicious packages tab**
- An explanation: downloads of packages OSV lists as malicious are refused, from the cache and from upstream; the first block of each is sent by email and as a `package.blocked` webhook.
- Table: package, repository, the record listing it as malicious (MAL- or GHSA id and summary), attempts, last requested by, last and first blocked, status (Blocked / Allowed; hover shows who allowed it and why).
- Administrator actions: "Allow…" opens a modal (a warning and a required reason; `POST /vulnerabilities/allowed`), "Block again" (Popconfirm; `DELETE /vulnerabilities/allowed?purl=`). Both are written to the audit log.
- When the server has blocking off (`vulnerabilities.block_malicious: false`), say so.

**Data sources**: `GET /vulnerabilities/summary`, `GET /vulnerabilities?…&sort&order`, `GET /vulnerabilities/export?as&levels&lang`, `GET /vulnerabilities/blocked`.

### 6.6 Repositories management `/ui/admin/repositories`

**Permissions**: `app:repositories read`; create/edit needs `write`; delete needs `delete`.

#### List
- Toolbar: search, format/type filters, a primary "Create Repository" button.
- Columns: name (linked), format icon + name, a type tag, storage, online (a Switch that toggles directly → `PUT /repositories/{name} {online}`), packages, size, URL (copyable; Docker also shows small tags for `:httpPort`/path/subdomain), the routing rule name, and the number of cleanup policies.
- Row actions: edit (Drawer), browse content (jumps to Browse), Usage, invalidate cache (proxy/group only, `POST /{name}/invalidate-cache`, a toast after confirming), delete (requires typing the name; `DELETE`; a note that "content and blobs are reclaimed on the next blob-gc run").
- Hovering a group row shows its members; a proxy row shows a red "Blocked" tag when `attributes.proxy.blocked` is set; there's currently no API exposing an in-progress autoBlock (a backend-side temporary block), so it's omitted.

#### Creation wizard `/ui/admin/repositories/new` (a full-page Steps flow)
**Step 1, choose a format**: 26 cards (icon, name, one-line description), with a search box at the top. Picking one advances to the next step.
**Step 2, choose a type**: three large cards — Hosted / Proxy / Group — each with a description and typical use case. The Group card is disabled when that format has no repositories yet, with a note "Create a hosted or proxy repository first."
**Step 3, configure** (a single form, split into sections):
- **Basics**: name (regex `^[A-Za-z0-9._-]+$`, validated live; 409 → "This name already exists"), storage (a Select from `GET /storages`, defaulting to `default`), online (on by default).
- **Proxy** (type=proxy): remoteUrl (pre-filled by format — Maven → `https://repo1.maven.org/maven2/`, npm → `https://registry.npmjs.org/`, Docker → `https://registry-1.docker.io`, PyPI → `https://pypi.org/`, Go → `https://proxy.golang.org`; Helm needs the user to fill it in; the rest per the table), contentMaxAge (minutes, default 1440; a "Cache forever" checkbox sets it to `-1`), metadataMaxAge (default 1440), negativeCacheTtl (default 1440; 0 disables it), upstream credentials (username/password, same password rules as 5.0.4), blocked, autoBlock (on by default).
- **Hosted** (type=hosted): a writePolicy radio group — `allow` (existing paths can be overwritten) / `allow_once` (default; a path can be written once, redeploying the same version is rejected, with a Maven SNAPSHOT exception — recommended) / `deny` (read-only, no uploads).
- **Group** (type=group): members as an ordered list — drag a row or use its up/down arrows to reorder, a delete button to remove — with a select below that adds a member at the end. Only non-group repositories of the same format are offered. The order is the resolution order, with a note explaining "members are asked from top to bottom; the first one that has the file answers." Deployments sent to the group go to its first hosted member.
- **Format-specific fields** (shown depending on Step 1's choice):
  - Maven: layoutPolicy (STRICT / PERMISSIVE), versionPolicy (RELEASE / SNAPSHOT / MIXED; only meaningful for hosted).
  - Docker: httpPort (0 = off), httpsPort + tlsCert/tlsKey (server-side PEM file paths), subdomain, pathEnabled (on by default), forceBasicAuth, indexType (HUB / REGISTRY / CUSTOM; proxy only). Each of the three access modes has a line explaining what its resulting URL looks like.
  - APT: distribution (default `stable`), component (`main`), signingKey (an ASCII-armored PGP private key textarea) + passphrase, flat (proxy).
  - YUM: repodataDepth (0–5), signingKey + passphrase.
  - Alpine: signingKey (an RSA private key, PEM), keyName (default `holiaokho.rsa.pub`).
  - Cargo: a downloadUrl template (can be left empty for proxy).
  - No other format has format-specific fields.
- **Signing key generator**: next to the signingKey field, a "Generate key" button → a Modal asking for name/email → `POST /system/pgp-key` (apt/yum) or `POST /system/rsa-key` (alpine) → returns `{privateKey, publicKey}`: the private key auto-fills the field, the public key is shown in the Modal for download/copy, with a reminder that "the public key will later be available at `<repoUrl>/repository-key.gpg` (for Alpine: `<repoUrl>/<keyName>`)."
- **Routing rule** (a Select, can be empty), **Cleanup policies** (multi-select; after creation, `PUT /cleanup-policies/{id}/repositories/{name}` is called once per selection).
- At the bottom: "Create" → `POST /repositories` → on success, redirects to that repository's detail page, on the Usage tab.

**Business logic**
- The submitted payload: `{name, format, type, storage, online, attributes: {proxy?|hosted?|group?, <format>?}}`; sections that aren't shown aren't sent.
- 400 `validation` errors show on the matching field (when the backend's message names a field, the front end maps it there; otherwise it shows at the top).
- The wizard's "Back" can revisit format/type; filled-in common fields are kept, and format-specific fields are cleared.

#### Single repository page `/ui/admin/repositories/:name`
Tabs:
- **Settings**: the same form as Step 3 (name/format/type shown read-only in grey); saving calls `PUT /repositories/{name}` (only `online` and `attributes` are sent); a proxy's password shows as `***`.
- **Content**: the Browse right column, embedded.
- **Usage**: see 6.7.
- **Stats**: three numbers — packages, assets, size — plus download counts (future); "Invalidate cache" and "Delete" live in a danger zone at the bottom of this tab.

### 6.7 The "Usage" Drawer/Tab (`<UsageSnippets format type url>`)

A pure front-end component with no API of its own. It generates 1–3 snippets per format, each with a title, a one-line explanation, and a code block (copyable). The URL comes from the repository's `url`; for Docker, the address depends on `attributes.docker`:
- Port mode: `<host>:<httpPort>/<image>`; path mode: `<host>/<repo>/<image>` (when `pathEnabled`); subdomain: `<subdomain>.<host>/<image>`; proxy repositories also get a daemon `registry-mirrors` snippet.
- Maven: a `settings.xml` `<mirror>` (group/proxy) or `<distributionManagement>` (hosted), plus a `<server>` credential using a token.
- npm: `.npmrc`'s `registry=` and `//host/repository/<name>/:_authToken=`; a scoped variant too.
- PyPI: `pip.conf`'s `index-url`, and `twine upload --repository-url`.
- Go: `GOPROXY=<url>` (with a note about `GONOSUMDB` / `GOSUMDB=sum.golang.org <url>/sumdb`).
- Helm: `helm repo add`; hosted repositories also get `helm push`/a curl upload.
- APT: one line for `sources.list` plus `curl <url>/repository-key.gpg | gpg --dearmor`; YUM: a `.repo` file with `gpgkey=`; Alpine: `/etc/apk/repositories` plus downloading the public key into `/etc/apk/keys/`.
- NuGet: `dotnet nuget add source`, `nuget push -ApiKey <token>`.
- Cargo: `.cargo/config.toml`'s `[registries]` sparse entry; `cargo login`.
- Composer: `composer config repositories.holiao composer <url>`. Conda: `channels`. CRAN: `options(repos=)`. RubyGems: `gem sources -a` / `gem push --host`. pub: `PUB_HOSTED_URL`. Terraform: `provider_installation network_mirror` / a module source. Git LFS: `.lfsconfig`. Hugging Face: `HF_ENDPOINT`. Ansible: `ansible.cfg`'s `server_list`. Conan: `conan remote add`. Swift: `swift package-registry set`. CocoaPods: `source`. p2: an Eclipse update-site URL. raw: curl upload/download examples.
- Every snippet ends with a line: "Credentials: use a User Token (username + token as the password)," linking to My Tokens.
- All snippets contain **language-agnostic** code; only the explanatory text goes through i18n.

### 6.8 Storages `/ui/admin/storages`

**Permissions**: `app:storages read/write`.

**Layout**: cards or a table: name, a type tag (fs / s3), location (fs shows a path; s3 shows `bucket/prefix @ endpoint`), a usage progress bar (`storage:<name>`'s usedBytes/quotaBytes from `/status/check`), blob count (if available), status (healthy / error message). A "Create Storage" button.

**Create Drawer** (`POST /storages`): name, a type radio; fs: path (an absolute server-side path, noted as "the path inside the container"); s3: endpoint, region, bucket, prefix, accessKey, secretKey, pathStyle (required for MinIO and most self-hosted S3). A "Test connection" button: `POST /storages/test` (sends the current form values, no need to save first; the backend writes, reads back, and deletes a probe blob) → `{ok, type, latencyMs}` shows a green check and the latency; `{ok:false, message}` shows the reason in red next to the button. The create button doesn't require testing first.

**Edit Drawer** (`PUT /storages/{name}`): can only change the quota (a GB input, 0 = unlimited) and the s3 credentials (also has "Test connection"; when `secretKey` is left as `***`, the backend tests using the stored value); path/bucket can't be changed (noted as "to change the location, create a new storage").

**Business logic**
- The `default` storage can't be deleted (there's no delete API for it; the UI doesn't offer one).
- When a quota is full, client uploads get a 507; the UI must show a red warning both on the storage card and on the Dashboard.
- A collapsible explanatory block: "A file that exists in several repositories is stored once. Deleted content is released by the blob-gc task; compact-blobs then frees the disk space."

### 6.9 Users `/ui/admin/users`

**Permissions**: `app:users`.

**List**: username, displayName, email, a source tag (local/ldap/oidc/rut), role tags, active (a Switch → `PUT /users/{u} {active}`), createdAt; toolbar search, a source filter, a "Create user" button. Row actions: edit, reset password (local only), Tokens, delete (requires typing the name; `admin` and yourself can't be deleted; `anonymous` is either hidden or shown but with its password uneditable).

**Create Drawer** (`POST /users`): username (same regex as repository names), displayName, email, password + confirmation + `<PasswordRules>`, roles (multi-select, `GET /roles`), active.
**Edit Drawer** (`PUT /users/{u}`): the same, minus password; when `source !== "local"`, show an Alert: "Synced from LDAP/OIDC. Roles from the mapping are applied at login; roles set here are added on top."
**Reset password Modal** (`PUT /users/{u}/password`): new password + `<PasswordRules>` (with the username fed in, to check "does not contain the username").
**Tokens Drawer**: the same as 6.12, but against `/users/{u}/tokens`.

**Business logic**
- 409 `user.exists` → an error on the username field.
- Removing your own `admin` role would lock you out: the front end pops a confirmation warning when you're about to remove it from yourself.
- The `admin` user can't be deactivated or deleted.

### 6.10 Roles `/ui/admin/roles`

**List**: id, name, description, number of privileges, a "built-in" tag for `admin`, `anonymous`, and `developer` (can't be deleted; their privileges can be edited, except `admin`'s).

**Create/Edit Drawer** (`POST /roles`, `PUT /roles/{id}`): id (fillable on create, same regex as above, auto-derived if left empty), name, description, and a **privilege editor**:
- Each table row is one privilege: `target` + `actions`.
- The target uses a two-part input: a **kind Select** ("Entire system" `*` / "Application area" `app:` / "Repository" `repo:` / "All repos of a format" `format:` / "Content selector" `selector:`) plus a **value**: for app, a Select (repositories, storages, users, roles, tasks, system, search, status, `*`); for repo, a Select (including `*`); for format, a Select; for selector, two Selects ("selector name @ repository or *").
- Actions are a checkbox group: read/write/delete/admin, or `*` (checking `*` disables the others).
- At the bottom, a live plain-language summary of "what this role can do" (e.g. "Read all repositories; administer users").
- Add a row, delete a row, duplicate a row.

**Business logic**
- At least one privilege is required before saving.
- A `selector:` target can only be chosen when a content selector exists; otherwise it shows a "Create a content selector first" link.

### 6.11 Content Selectors `/ui/admin/content-selectors`

**List**: name, expression (monospace, truncated), description, which roles reference it (the front end cross-references the roles list for `selector:<name>@`).

**Drawer** (`POST`/`PUT /content-selectors`): name, description, expression (a CodeMirror-style single/multi-line editor with a syntax help panel: supports `format == "maven"`, `path =^ "/com/acme/"` (prefix), `path =~ "regex"`, `and`/`or`/`not`, and parentheses).
**Test panel** (inside the Drawer): enter a format and path → `POST /content-selectors/test {expression, format, path, coordinate?}` → `{matches}` shows a green check "Matches" or a grey cross "Does not match"; an expression syntax error (400) shows under the expression field.

**Business logic**: deleting a selector that's referenced by a role warns which roles it affects.

### 6.12 My Tokens `/ui/me/tokens` (personal menu)

**Permissions**: any logged-in user (hidden when anonymous).

**List**: name, prefix (`hlk_xxxx…`), createdAt, lastUsedAt, expiresAt (expired rows are grey with "Expired"); a "Create token" button; a row action "Revoke" (with confirmation).

**Create Modal** (`POST /me/tokens {name, expiresAt?}`): name (required), expiry (a radio: 30 days / 90 days / 1 year / never / a custom date). On success, it switches to a **one-time reveal screen**: a large monospace secret, a copy button, a red warning "You will not be able to see this secret again — store it now," and an "I have saved it" checkbox that must be ticked before closing.

**Explanatory section**: using the token as a password with any client (Basic auth), `Authorization: Bearer <token>`, NuGet's ApiKey, cargo's token, npm's `_authToken` — with one example line for each.

**Business logic**: the secret only exists in that Modal's state and is cleared on close; revoking removes it from the list immediately.

### 6.13 Auth Settings `/ui/admin/auth`

**Permissions**: `app:system`. A single page with left-hand anchor navigation and section forms on the right, **each section saved independently** (all hitting the same `PUT /auth/settings`; the front end merges the current settings with that section's changes and sends the whole thing; secret fields stay `***`).

**Sections**
1. **Realms & anonymous**: a draggable, reorderable list of realms (local, ldap); a disabled realm can be removed; anonymous access status (shown read-only, sourced from the config file's `auth.anonymous_enabled`, with a note "change it in the config file or an environment variable") plus a summary of the current `anonymous` role's privileges (linked to Roles).
2. **Default roles**: multi-select.
3. **Password policy**: minLength (≥8), maxLength, four require-toggles, disallowUsername, disallowCommon; a live example on the right, "A valid password looks like this."
4. **LDAP**: an enabled toggle; url (`ldap://`/`ldaps://`), startTLS, bindDN, bindPassword, userBaseDN, userFilter (default `(uid={username})`), userSubtree, emailAttr, displayNameAttr, a group section (groupBaseDN, groupFilter, groupNameAttr, memberAttr), a role-mapping table (LDAP group → role, multi-select), timeout. Buttons: "Test connection" and "Test login" (`POST /auth/ldap/test {config: <the LDAP section's current form values>, username, password}` — tests directly against the unsaved config and shows the DN, email, and groups found, plus the roles they map to).
5. **OIDC**: enabled; issuer, clientId, clientSecret, scopes (tags), usernameClaim, groupsClaim, a role-mapping table, autoCreate; a read-only redirect URL, `<origin>/api/v1/auth/oidc/callback` (copyable), for registering with the IdP.
6. **Rut Auth**: enabled; the header name, autoCreate, defaultRoles; a red Alert, "Enable only behind a reverse proxy that strips this header from client requests, and only when `server.trusted_proxies` is configured," with the current trusted_proxies shown read-only (from `/system/config`).

**Business logic**
- Saving with LDAP/OIDC enabled but required fields empty is blocked on the front end.
- Removing `ldap` from the realm list while LDAP is still enabled → a note: "LDAP is enabled but not in the realm order, so it will not be used for login."
- Changes take up to 30 seconds to reach the backend cache (the backend's settings cache has a 30s TTL); the save toast says "Saved — takes effect within 30 seconds."

### 6.14 Tasks `/ui/admin/tasks`

**List** (`GET /tasks`, polled every 3s while anything is running): name (translated, with the raw name alongside), description, schedule (shows the cron expression plus a plain-language reading when `cron` is set; otherwise shows `interval`), an enabled Switch, lastRun (relative time + a colour dot for `lastStatus`), nextRun, a running spinner; row actions: "Run now" (`POST /tasks/{name}/run` → 202 → a toast, then polling starts), "Schedule."

**Schedule Modal** (`PUT /tasks/{name}/schedule {cron?, enabled}`): a radio for "Default interval" / "Cron"; a cron input with common templates (Daily 02:00, Hourly, Sunday 03:00), a plain-language reading, and a "Next 5 runs" preview (computed by the server — `GET /tasks/cron-preview?expr=` — since tasks run on its clock; the modal names the server's time zone and, when it differs, shows each run in the user's time next to the server's); an enabled toggle.

**Run history** (the lower half of the page, or a tab; `GET /tasks/runs?task=`, filtered by task with a searchable dropdown): time, task, a status tag (success/failed/running), duration, an expandable row showing the `log` (monospace, dark background).

**Built-in tasks and their translations**: `blob-gc` (reclaims unreferenced blobs, a soft delete), `compact-blobs` (actually deletes files to free space), `cleanup-policies` (applies cleanup rules), `prune-expired` (clears expired caches/sessions/tokens), `rebuild-indexes` (rebuilds each format's index), `backup` (appears only when configured).

### 6.15 Cleanup Policies `/ui/admin/cleanup`

**List**: name, format (`*` shows as "All"), a criteria summary (e.g. "not downloaded in 30 days · keep newest 5 per name · releases only"), the repositories it's applied to; a "Create policy" button.

**Drawer** (`POST`/`PUT /cleanup-policies`): name, format (a Select, including "All"), criteria: lastDownloadedDays, lastUpdatedDays (note that these two are OR'd together), keepLatest (keep the newest N versions per name), nameRegex, versionRegex, prerelease (a radio: Any / Only prereleases / Only releases).
**Preview panel** (at the bottom of the Drawer, or a separate Modal): pick a repository → `POST /cleanup-policies/{id}/preview?repository=<name>&limit=200` → `{wouldDelete, packages[]}` shows "Would delete N packages" and the list (namespace/name/version, last downloaded), paginated. **An unsaved policy can't be previewed** (save it first).
**Assignment**: a multi-select of repositories inside the Drawer (only same-format ones are listed); changes are sent as diffs via `PUT`/`DELETE /cleanup-policies/{id}/repositories/{name}`.

**Business logic**
- At least one criterion is required to save.
- The rule only takes effect when the `cleanup-policies` task runs: the top of the Drawer shows when it will next run (from `/tasks`), with a "Run cleanup now" button (jumps to Tasks to run it, after confirming).
- This is the easiest place to accidentally delete the wrong things: highlight a preview result over 100 in a bright yellow.

### 6.16 Routing Rules `/ui/admin/routing-rules`

**List**: name, a mode tag (allow in green / block in red), number of matchers (shown on hover), description, the repositories using it (the front end cross-references repositories' `routingRuleId`).

**Drawer**: name, description, a mode radio (`block`: reject any path matching a matcher; `allow`: only serve paths that match one), a dynamic list of matchers (regex, with live syntax checking per row).
**Test panel**: enter a path → `POST /routing-rules/test {mode, matchers, path}` (sends the current form values, no need to save first) → `{allowed}` shows "Allowed"/"Blocked" plus the matcher that hit highlighted in amber.

**Business logic**: deleting a rule that's in use by repositories warns and lists them; after deletion, those repositories' `routingRuleId` becomes null (handled by the backend).

### 6.17 Backup / Restore `/ui/admin/backup`

**Permissions**: `app:system admin` (higher than write).

**Layout**
- A **scheduled backup** card (read-only, from `/system/config`'s `backup` section): directory, whether blobs are included, retention count, cron; a note "change it in the config file"; alongside it, the `backup` task's most recent run.
- **Download a backup now**: an "Include blobs" checkbox (with a warning that the size is roughly the total storage in use) → `GET /system/backup?blobs=true`, downloaded by the browser (an `<a download>`, with the cookie sent).
- A **restore** danger zone (red border): drop a `.tar.gz` (a Dragger) → a first confirmation Modal explaining "Restoring replaces the entire database and blob store with the archive contents. Stop client traffic first." → a second step requiring `RESTORE` to be typed before the button enables → `POST /system/restore` as multipart, with an upload progress bar and a "processing" spinner (can take several minutes) → forces a fresh login once complete.

### 6.18 Webhooks `/ui/admin/webhooks`

**List**: name, url, event tags, a repository filter (empty = all), an enabled Switch, createdAt; a "Create" button; row actions: edit, "Send test" (`POST /webhooks/{id}/test` → `{queued, message}`; delivery is asynchronous, so the toast says "Test event queued — check Logs for the delivery result"), delete.

**Drawer**: name, url (https recommended), a multi-select of events (`asset.created`, `asset.deleted`, `package.deleted`, `repository.create`, `repository.update`, `repository.delete`, `repository.invalidate_cache`, `user.create`, `user.update`, `user.delete`, `task.failed`, `*`), repository (a Select, can be empty), secret (a password field, following the `***` rule), enabled.
**Explanatory section**: a sample JSON payload, the signature header `X-Holiaokho-Signature: sha256=<HMAC>`, and a signature-verification example each in Node and Go.

### 6.19 Email `/ui/admin/email`

A single form (`GET`/`PUT /email`): enabled, host, port, username, password (`***`), from, startTls/ssl (a mutually exclusive radio: none / STARTTLS / SSL), recipients (tags; they receive task-failure and quota-warning notifications). A "Send test email" button (`POST /email/test {to}` → `{sent}`, shown as a toast).

### 6.20 System `/ui/admin/system/*`

- **Health** `/health`: a table version of the Dashboard's cards: check name, status, message, (for storage) usage; a "Re-check" button; polled every 30s.
- **Information** `/info` (`GET /system/info`): grouped Descriptions: version (version, commit, buildDate), runtime (`go`, `os`/`arch`, `cpus`, `hostname`, `pid`, `uptime`, `memory`: heapAllocBytes/sysBytes/goroutines/numGC), database (version, connection count, row counts per table), storages, formats (26 tags), dependencies (a collapsible table). A "Copy as text" button, top right.
- **Logs** `/logs` (`GET /system/logs?tail=500`): a dark monospace area, colour-coded by level (ERROR red, WARN yellow, INFO grey, DEBUG faint); at the top: a tail-length Select (200/500/2000), a level filter (front-end), a keyword filter (front-end), an "Auto refresh" Switch (every 2s, auto-scrolls to the bottom while on), "Copy all"; next to it, a **log level** Select (`GET`/`PUT /system/log-level`: debug/info/warn/error, takes effect immediately, with a note "resets on restart").
- **Configuration** `/config` (`GET /system/config`): read-only YAML (syntax highlighted); secrets are masked by the backend; a note at the top, "Read-only. Secrets are masked. Change values in the config file or with `HOLIAOKHO_*` environment variables, then restart."
- **Support ZIP** `/support`: a single download button (`GET /system/support-zip`) plus a list of what's inside (system information, config (masked), recent logs, health, the repository list) plus "without user data or blobs."
- **Audit Log** `/audit` (`GET /audit?limit=`): time, actor (username; `anonymous`/a token prefix/`system`), action (translated, e.g. `repository.create` → "created repository"), the target's type and name (clickable), detail (JSON, in an expandable row); filters by action/actor (front-end), a limit Select; "Load more."

### 6.21 Personal menu (top bar, right)
- Shows the username plus a `via` tag (session/token/ldap/oidc).
- Items: My Tokens, Change password (only for local users; hidden when `via` is ldap/oidc), Language, Theme, Log out.
- When anonymous, this becomes a "Log in" button.

### 6.22 Notification centre (top-bar bell)
A pure front-end aggregation with no dedicated API: it polls `/status/check` and `/tasks` every 60s, turning unhealthy checks and tasks with `lastStatus failed` into notifications; clicking one jumps to the relevant page; read state is stored in localStorage. Users without permission (non-admins) don't see the bell at all.

---

## 7. Key flows (worth a flow diagram / page-by-page mockups)

1. **First install**: log in (admin/default password) → forced password change → the Dashboard's empty-state onboarding → create the first proxy (e.g., Maven Central) → copy the `settings.xml` from Usage.
2. **A developer finding a package**: land on Browse anonymously → search "gson" → see which repository has it and which versions → copy the download URL or the client setup.
3. **A CI token**: a developer logs in → My Tokens → creates one → copies the secret → drops it into a CI secret.
4. **An admin sets up LDAP**: Auth Settings → fills in LDAP → tests the login successfully → sets up role mapping → adds ldap to the realm order.
5. **Running out of space**: a Dashboard quota warning → create a rule under Cleanup Policies → preview it → assign it → run it → check the result under Tasks → compact-blobs reclaims the space.

---

## 8. Localisation

- Languages: **en, zh-TW, zh-CN, ja, ko**, switchable top right, with the preference remembered; the default follows the browser.
- Backend errors return `{code, message, params}`; the front end looks up a translation by `code` (e.g., `repo.redeploy_denied` → "This repository does not allow redeploying an existing version"). Leave room for **two lines** of error text in the design.
- Text length: Japanese/Chinese are usually shorter, English/Korean longer; buttons and table columns need to fit +30%.
- Date and time: formatted per locale, shown as relative time ("3 minutes ago") with the absolute time on hover.
- File sizes: KB/MB/GB, formatted per locale.
- RTL is not needed.

---

## 9. Layout and devices

- **Desktop-first** (admins are always at a desk), 1280–1920 wide; there's a lot of tabular data, so use the width well.
- Tablet (768–1024): the nav collapses, tables scroll horizontally, forms go single-column.
- Phone (<480): only Browse / Search / Login / My Tokens need to work; admin pages can degrade gracefully without needing pixel-perfect polish.
- **Both light and dark themes** are first-class citizens (developers often prefer dark).
- Accessibility: contrast ≥ 4.5:1, every form and table operable by keyboard, visible focus states, icons always paired with text labels.

---

## 10. States and details (needed on every list/form)

- Loading (skeletons), empty states (with a next action), errors (retryable, showing the error code), insufficient permissions (hidden, or a friendly explanation).
- Destructive actions (deleting a repository, deleting a user, restoring a backup, running a cleanup): a modal requiring the name to be typed or a checkbox confirmed.
- Large data volumes: there can be 100+ repositories, 100k+ packages, and even more assets — tables need pagination or virtual scrolling, and search needs debouncing.
- There will be a lot of copy-to-clipboard components (URLs, commands, checksums, tokens): a single, consistent "copyable text" component with a success indicator is needed.
- Real-time-ish behaviour: running tasks, log tailing, and health cards all need to poll and update (no WebSocket needed).
- Docker is a special case: a repository can have several access addresses (port mode `host:5000/image`, path mode `host/repo/image`, subdomain) — Usage needs to keep these clearly separated.

---

## 11. Technical constraints (designers should know these)

- The SPA is mounted at `/ui/` (other paths are for package clients: `/repository/...`, `/v2/...`); it's built and bundled into the Go binary — there's no SSR.
- All data comes from `/api/v1` (the OpenAPI spec is at `/api/v1/openapi.yaml`); there's no GraphQL and no WebSocket.
- Authentication: a session cookie (from the login form) or a Bearer token; OIDC login is a full-page redirect that comes back through `/api/v1/auth/oidc/callback` and then on to `next`.
- Anonymous users may have read access: the UI has to be able to render Browse/Search while logged out.
- Icons: each format's icon needs to be designed from scratch (no third-party trademarks); a simple letter or geometric shape plus that ecosystem's conventional colour works (Java orange, npm red, Docker blue, Python yellow/blue, Go cyan, Rust orange, Ruby red…).

---

## 12. Expected deliverables

1. **A design system**: colour (light and dark), type scale, spacing, corner radius, shadows, status colours — mapped to Ant Design token names.
2. **Logo / wordmark**: 2–3 proposals, plus a favicon.
3. **Page layouts**: a desktop version of every page in Section 6 (with a tablet version for the key pages); page-by-page mockups for the five flows in Section 7.
4. **Component specs**: the format icon set (26 icons), type tags (hosted/proxy/group), copyable text, health-status cards, the privilege editor, the cron editor, the one-time-secret modal, the delete-preview list.
5. **Common styles** for empty/error/loading states.
6. **A design write-up** the front-end engineers can build from directly (including i18n and dark-mode notes).

---

## Appendix A: comparison with the Nexus UI (a reference for "what not to look like")

Nexus 3's UI is a settings tree on the left (Repository/Security/Support/System) with a dense but dated ExtJS table on the right, and it's unfriendly to developers (no "how to use this" guidance, and search scattered around). We keep its strength — thorough coverage of admin tasks — while improving on: the experience for non-admins in Browse and Search, a "how to use it" snippet for every repository, preview-first cleanup, and a modern dark mode with real localisation.

## Appendix B: suggested zh-TW terminology

Repository → 儲存庫 (or just "Repository"), Hosted → 自有, Proxy → 代理快取, Group → 群組, Package → 套件, Asset → 檔案, Storage → 儲存空間, Cleanup Policy → 清理規則, Routing Rule → 路由規則, Content Selector → 內容選擇器, Token → 存取權杖, Realm → 認證來源, Audit Log → 稽核記錄, Blob → 內容區塊. Other languages are handled during translation, but the design mockups can demonstrate copy in just English and zh-TW.

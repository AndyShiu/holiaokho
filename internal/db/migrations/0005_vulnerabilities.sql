-- Known vulnerabilities in stored packages, looked up in OSV (osv.dev) by
-- package URL. A vulnerability is stored once however many packages it
-- affects; package_vulnerabilities says which ones.
CREATE TABLE vulnerabilities (
    id         text PRIMARY KEY,               -- GHSA-…, CVE-…, PYSEC-…, GO-…
    aliases    text[] NOT NULL DEFAULT '{}',
    summary    text NOT NULL DEFAULT '',
    -- CRITICAL / HIGH / MODERATE / LOW, or UNKNOWN when the record gives
    -- neither a rating nor a CVSS vector to derive one from.
    severity   text NOT NULL DEFAULT 'UNKNOWN',
    score      numeric(3,1),                   -- CVSS base score, when known
    published  timestamptz,
    modified   timestamptz,                    -- OSV's, to know when to refetch
    -- The versions that fix it, per affected package: {"pkg:npm/x": ["1.2.3"]}.
    -- Kept so a rescan only has to fetch records that are new or changed.
    fixed      jsonb NOT NULL DEFAULT '{}',
    fetched_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE package_vulnerabilities (
    package_id bigint NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
    vuln_id    text   NOT NULL REFERENCES vulnerabilities(id) ON DELETE CASCADE,
    -- Versions that fix it, as OSV lists them for this package.
    fixed_in   text[] NOT NULL DEFAULT '{}',
    first_seen timestamptz NOT NULL DEFAULT now(),
    -- Set once the finding has been sent by email / webhook, so each one is
    -- announced exactly once however many scans see it again.
    notified_at timestamptz,
    PRIMARY KEY (package_id, vuln_id)
);
CREATE INDEX package_vulnerabilities_vuln_idx ON package_vulnerabilities (vuln_id);

-- When each package was last checked. NULL = never, which is also what puts
-- a newly arrived package at the front of the queue.
ALTER TABLE packages ADD COLUMN vuln_scanned_at timestamptz;
-- Whether that check could actually be made. A package OSV cannot be asked
-- about (a format it does not cover, a NuGet id whose casing is unknown) is
-- recorded as not covered — never as scanned and clean.
ALTER TABLE packages ADD COLUMN vuln_covered boolean;
CREATE INDEX packages_vuln_scanned_idx ON packages (vuln_scanned_at NULLS FIRST);

-- The outcome of the last scan, so the UI can say "could not reach OSV"
-- instead of showing stale results as if they were current.
CREATE TABLE vuln_scan_state (
    id           int PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_run_at  timestamptz,
    last_ok_at   timestamptz,
    last_error   text NOT NULL DEFAULT ''
);
INSERT INTO vuln_scan_state (id) VALUES (1);

-- Malicious packages: OSV's MAL- records (the OpenSSF malicious-packages
-- database) and advisories classed CWE-506. A vulnerability can be upgraded
-- away from; a malicious package is refused at download.
ALTER TABLE vulnerabilities ADD COLUMN malicious boolean NOT NULL DEFAULT false;
UPDATE vulnerabilities SET malicious = true
 WHERE id LIKE 'MAL-%' OR EXISTS (SELECT 1 FROM unnest(aliases) a WHERE a LIKE 'MAL-%');
-- Stored records are refetched when OSV modifies them; until then, a record
-- classed CWE-506 would not be known as malicious. Refetch them all once.
UPDATE vulnerabilities SET modified = 'epoch';

-- Downloads refused because the package is malicious, one row per package
-- version (purl) and repository: who tried, how often, and when.
CREATE TABLE malware_blocks (
    purl        text NOT NULL,
    repository  text NOT NULL,
    format      text NOT NULL,
    name        text NOT NULL,
    version     text NOT NULL,
    vuln_id     text NOT NULL,
    summary     text NOT NULL DEFAULT '',
    attempts    integer NOT NULL DEFAULT 1,
    first_at    timestamptz NOT NULL DEFAULT now(),
    last_at     timestamptz NOT NULL DEFAULT now(),
    last_user   text NOT NULL DEFAULT '',
    PRIMARY KEY (purl, repository)
);
CREATE INDEX malware_blocks_last_idx ON malware_blocks (last_at DESC);

-- Package versions an administrator has let through anyway (a wrong
-- classification), by purl so the decision holds in every repository.
CREATE TABLE malware_allowed (
    purl       text PRIMARY KEY,
    reason     text NOT NULL,
    allowed_by text NOT NULL,
    allowed_at timestamptz NOT NULL DEFAULT now()
);

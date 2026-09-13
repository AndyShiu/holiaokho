-- OCI artifacts can point at another artifact through a `subject` field: a
-- cosign signature, an SBOM or a build attestation is its own manifest that
-- declares which image it describes. The referrers API answers "what points at
-- this digest", which cannot be derived from the manifests themselves without
-- reading every one of them, so the relationship is indexed as it arrives.
CREATE TABLE oci_referrers (
    repo_id         uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    subject_digest  text NOT NULL,
    manifest_digest text NOT NULL,
    -- The repository name inside the registry (e.g. "library/alpine"), because
    -- referrers are scoped to a name, not just to a repository.
    image_name      text NOT NULL,
    artifact_type   text NOT NULL DEFAULT '',
    media_type      text NOT NULL,
    size            bigint NOT NULL DEFAULT 0,
    annotations     jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repo_id, image_name, subject_digest, manifest_digest)
);

-- The only query this table serves: everything pointing at one digest.
CREATE INDEX oci_referrers_subject_idx
    ON oci_referrers (repo_id, image_name, subject_digest);

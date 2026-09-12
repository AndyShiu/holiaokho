-- Routing rules: allow/block request paths per repository (Nexus "Routing Rules").
CREATE TABLE routing_rules (
    id          uuid PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    mode        text NOT NULL DEFAULT 'block',   -- allow | block
    matchers    jsonb NOT NULL DEFAULT '[]',     -- array of regex strings
    created_at  timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE repositories ADD COLUMN routing_rule_id uuid REFERENCES routing_rules(id) ON DELETE SET NULL;

-- Content selectors: named path/format expressions used as privilege targets.
CREATE TABLE content_selectors (
    id          uuid PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    expression  text NOT NULL,                   -- e.g. format == "maven" and path =^ "/com/acme/"
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Webhooks: HTTP callbacks on repository / audit events.
CREATE TABLE webhooks (
    id          uuid PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    url         text NOT NULL,
    secret      text NOT NULL DEFAULT '',
    events      jsonb NOT NULL DEFAULT '[]',     -- e.g. ["asset.created","package.deleted","audit"]
    repository  text NOT NULL DEFAULT '',        -- '' = all repositories
    enabled     boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Task schedules persisted so cron expressions survive restarts.
CREATE TABLE task_schedules (
    task_name text PRIMARY KEY,
    cron      text NOT NULL,
    enabled   boolean NOT NULL DEFAULT true
);

-- Storage quotas (bytes; 0 = none) and soft-deleted blobs.
ALTER TABLE storages ADD COLUMN quota_bytes bigint NOT NULL DEFAULT 0;
ALTER TABLE blobs ADD COLUMN deleted_at timestamptz;

-- Holiaokho initial schema. See docs/holiaokho-architecture-draft.md §7.

CREATE TABLE storages (
    id          uuid PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    type        text NOT NULL,
    config      jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE repositories (
    id          uuid PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    format      text NOT NULL,
    type        text NOT NULL,
    storage_id  uuid NOT NULL REFERENCES storages(id),
    online      boolean NOT NULL DEFAULT true,
    attributes  jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX repositories_format_idx ON repositories(format);

CREATE TABLE blobs (
    digest      text PRIMARY KEY,
    size        bigint NOT NULL,
    storage_id  uuid NOT NULL REFERENCES storages(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    ref_count   bigint NOT NULL DEFAULT 0
);
CREATE INDEX blobs_unreferenced_idx ON blobs(created_at) WHERE ref_count = 0;

CREATE TABLE packages (
    id          bigserial PRIMARY KEY,
    repo_id     uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    namespace   text NOT NULL DEFAULT '',
    name        text NOT NULL,
    version     text NOT NULL,
    attrs       jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    last_downloaded_at timestamptz,
    UNIQUE (repo_id, namespace, name, version)
);
CREATE INDEX packages_name_idx ON packages(name);
CREATE INDEX packages_repo_name_idx ON packages(repo_id, namespace, name);

CREATE TABLE assets (
    id          bigserial PRIMARY KEY,
    repo_id     uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    package_id  bigint REFERENCES packages(id) ON DELETE SET NULL,
    path        text NOT NULL,
    blob_digest text REFERENCES blobs(digest),
    size        bigint NOT NULL DEFAULT 0,
    content_type text NOT NULL DEFAULT '',
    attrs       jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    cache_expires_at   timestamptz,
    last_downloaded_at timestamptz,
    negative    boolean NOT NULL DEFAULT false,
    UNIQUE (repo_id, path)
);
CREATE INDEX assets_package_idx ON assets(package_id);
CREATE INDEX assets_blob_idx ON assets(blob_digest);
CREATE INDEX assets_repo_prefix_idx ON assets(repo_id, path text_pattern_ops);

CREATE TABLE users (
    id            uuid PRIMARY KEY,
    username      text NOT NULL UNIQUE,
    email         text NOT NULL DEFAULT '',
    display_name  text NOT NULL DEFAULT '',
    password_hash text NOT NULL DEFAULT '',
    source        text NOT NULL DEFAULT 'local',
    active        boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE roles (
    id          text PRIMARY KEY,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    privileges  jsonb NOT NULL DEFAULT '[]',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_roles (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id text NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE tokens (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text NOT NULL,
    prefix       text NOT NULL,
    hash         text NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz,
    last_used_at timestamptz
);

CREATE TABLE sessions (
    id         text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE TABLE cleanup_policies (
    id        uuid PRIMARY KEY,
    name      text NOT NULL UNIQUE,
    format    text NOT NULL DEFAULT '',
    criteria  jsonb NOT NULL DEFAULT '{}'
);

CREATE TABLE repo_cleanup_policies (
    repo_id   uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    policy_id uuid NOT NULL REFERENCES cleanup_policies(id) ON DELETE CASCADE,
    PRIMARY KEY (repo_id, policy_id)
);

CREATE TABLE task_runs (
    id          bigserial PRIMARY KEY,
    task_name   text NOT NULL,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    status      text NOT NULL DEFAULT 'running',
    log         text NOT NULL DEFAULT ''
);
CREATE INDEX task_runs_name_idx ON task_runs(task_name, started_at DESC);

CREATE TABLE settings (
    key   text PRIMARY KEY,
    value jsonb NOT NULL
);

CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    actor       text NOT NULL,
    action      text NOT NULL,
    target_type text NOT NULL DEFAULT '',
    target_id   text NOT NULL DEFAULT '',
    detail      jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_at_idx ON audit_log(at DESC);

CREATE TABLE tenants (
    id         BIGSERIAL PRIMARY KEY,
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TYPE video_status AS ENUM ('uploaded', 'transcoding', 'ready', 'failed');

CREATE TABLE videos (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    source_path TEXT NOT NULL,
    status      video_status NOT NULL DEFAULT 'uploaded',
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_videos_tenant ON videos(tenant_id);

CREATE TABLE renditions (
    id            BIGSERIAL PRIMARY KEY,
    video_id      BIGINT NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    height        INT NOT NULL,
    bitrate_kbps  INT NOT NULL,
    manifest_path TEXT NOT NULL,
    UNIQUE (video_id, height)
);

INSERT INTO tenants (slug, name) VALUES
    ('demo',   'Demo Tenant'),
    ('acme',   'Acme Media');

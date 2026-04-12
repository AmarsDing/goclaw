-- DreamWeaver runtime persistence
-- Migration 000050

CREATE TABLE dreamweaver_hook_configs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    hook_id     TEXT NOT NULL,
    event       TEXT NOT NULL,
    mode        TEXT NOT NULL,
    priority    INT NOT NULL DEFAULT 0,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    matcher     JSONB NOT NULL DEFAULT '{}',
    handler     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_dw_hook_configs_unique ON dreamweaver_hook_configs(tenant_id, agent_id, hook_id);
CREATE INDEX idx_dw_hook_configs_event ON dreamweaver_hook_configs(tenant_id, agent_id, event);

CREATE TABLE dreamweaver_audit_log (
    id              TEXT PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    agent_id        UUID REFERENCES agents(id) ON DELETE CASCADE,
    user_id         VARCHAR(255) NOT NULL DEFAULT '',
    run_id          TEXT NOT NULL DEFAULT '',
    tool_name       TEXT NOT NULL DEFAULT '',
    action          TEXT NOT NULL,
    allowed         BOOLEAN NOT NULL DEFAULT false,
    reason          TEXT NOT NULL DEFAULT '',
    classification  JSONB NOT NULL DEFAULT '{}',
    request_payload JSONB NOT NULL DEFAULT '{}',
    decision_payload JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_dw_audit_agent_created ON dreamweaver_audit_log(tenant_id, agent_id, created_at DESC);
CREATE INDEX idx_dw_audit_run_id ON dreamweaver_audit_log(tenant_id, run_id);

CREATE TABLE dreamweaver_spirit_profiles (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id),
    user_id           VARCHAR(255) NOT NULL,
    name              TEXT NOT NULL DEFAULT '',
    language          TEXT NOT NULL DEFAULT '',
    style             JSONB NOT NULL DEFAULT '{}',
    preferences       JSONB NOT NULL DEFAULT '{}',
    agent_affinity    JSONB NOT NULL DEFAULT '{}',
    work_context      JSONB NOT NULL DEFAULT '{}',
    interaction_count INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_dw_spirit_profiles_user ON dreamweaver_spirit_profiles(tenant_id, user_id);

CREATE TABLE dreamweaver_topics (
    id          TEXT PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    topic       TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    tags        JSONB NOT NULL DEFAULT '[]',
    sources     JSONB NOT NULL DEFAULT '[]',
    score       DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_dw_topics_unique ON dreamweaver_topics(tenant_id, agent_id, user_id, topic);
CREATE INDEX idx_dw_topics_agent_user ON dreamweaver_topics(tenant_id, agent_id, user_id);

CREATE TABLE dreamweaver_daily_logs (
    id          TEXT PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    log_date    DATE NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    sessions    JSONB NOT NULL DEFAULT '[]',
    tool_calls  INT NOT NULL DEFAULT 0,
    tokens      INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_dw_daily_logs_unique ON dreamweaver_daily_logs(tenant_id, agent_id, user_id, log_date);
CREATE INDEX idx_dw_daily_logs_agent_user ON dreamweaver_daily_logs(tenant_id, agent_id, user_id, log_date DESC);

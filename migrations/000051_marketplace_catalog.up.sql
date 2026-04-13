-- Marketplace catalog (authoritative when PG is enabled), audit trail, entitlements.

CREATE TABLE IF NOT EXISTS marketplace_packages (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT '',
  payload JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_marketplace_packages_tenant ON marketplace_packages(tenant_id);

CREATE TABLE IF NOT EXISTS marketplace_reviews (
  id TEXT PRIMARY KEY,
  package_id TEXT NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_marketplace_reviews_pkg ON marketplace_reviews(package_id);

CREATE TABLE IF NOT EXISTS marketplace_audit_events (
  id BIGSERIAL PRIMARY KEY,
  package_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL DEFAULT '',
  actor_user_id TEXT NOT NULL DEFAULT '',
  action TEXT NOT NULL,
  old_state TEXT,
  new_state TEXT,
  note TEXT,
  detail JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_marketplace_audit_pkg ON marketplace_audit_events(package_id);
CREATE INDEX IF NOT EXISTS idx_marketplace_audit_created ON marketplace_audit_events(created_at DESC);

CREATE TABLE IF NOT EXISTS marketplace_entitlements (
  id BIGSERIAL PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  user_id TEXT NOT NULL DEFAULT '',
  package_id TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT 'manual',
  active_until TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, user_id, package_id)
);

CREATE INDEX IF NOT EXISTS idx_marketplace_ent_tenant ON marketplace_entitlements(tenant_id);
CREATE INDEX IF NOT EXISTS idx_marketplace_ent_pkg ON marketplace_entitlements(package_id);

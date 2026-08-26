CREATE TABLE IF NOT EXISTS api_routes (
  subdomain TEXT PRIMARY KEY,
  owner_id TEXT NOT NULL UNIQUE,
  api_url TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_api_routes_owner_id ON api_routes(owner_id);


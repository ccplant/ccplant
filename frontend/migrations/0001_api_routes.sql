CREATE TABLE IF NOT EXISTS api_route_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  subdomain TEXT NOT NULL,
  api_url TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  actor_id TEXT,
  change_reason TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_api_route_events_latest
  ON api_route_events(subdomain, id DESC);

CREATE TRIGGER IF NOT EXISTS api_route_events_prevent_update
BEFORE UPDATE ON api_route_events
BEGIN
  SELECT RAISE(ABORT, 'api_route_events is append-only');
END;

CREATE TRIGGER IF NOT EXISTS api_route_events_prevent_delete
BEFORE DELETE ON api_route_events
BEGIN
  SELECT RAISE(ABORT, 'api_route_events is append-only');
END;

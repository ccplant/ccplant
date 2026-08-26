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


import type { NativeSession } from "../types";

interface Props {
  sessions: NativeSession[];
}

export function SessionsCard({ sessions }: Props) {
  return (
    <section className="card">
      <header className="card__header">
        <h2>Sessions</h2>
        <span className="pill pill--neutral">{sessions.length}</span>
      </header>
      {sessions.length === 0 ? (
        <div className="state state--empty state--inline">
          <p>No active sessions on this host.</p>
        </div>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>Status</th>
              <th>Session</th>
              <th>Activity</th>
              <th>Process</th>
            </tr>
          </thead>
          <tbody>
            {sessions.map((session) => (
              <tr key={session.id}>
                <td>
                  <span className={`pill pill--${statusPill(session.status)}`}>
                    {session.status || "—"}
                  </span>
                </td>
                <td className="session-summary">
                  <strong>{session.description || shortId(session.id)}</strong>
                  <span className="mono" title={session.id}>{session.description ? shortId(session.id) : session.id}</span>
                  {session.status_message && <span className="session-summary__error">{session.status_message}</span>}
                  {Object.keys(session.tags ?? {}).length > 0 && (
                    <span className="session-summary__tags">
                      {Object.entries(session.tags).map(([key, value]) => <span className="chip" key={key}>{key}={value}</span>)}
                    </span>
                  )}
                </td>
                <td title={formatDate(session.last_message_at || session.updated_at || session.started_at)}>{formatActivity(session.last_message_at || session.updated_at || session.started_at)}</td>
                <td className="mono">{session.pid ? `PID ${session.pid}` : "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function shortId(id: string): string {
  return id.length > 16 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id;
}

function statusPill(status: string): "ok" | "warn" | "bad" | "neutral" {
  switch (status) {
    case "running":
    case "active":
    case "stable":
      return "ok";
    case "error":
    case "failed":
      return "bad";
    case "starting":
    case "creating":
    case "queued":
    case "terminating":
    case "stopping":
      return "warn";
    default:
      return "neutral";
  }
}

function formatDate(value: string): string {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function formatActivity(value: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const seconds = Math.round((date.getTime() - Date.now()) / 1000);
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  if (Math.abs(seconds) < 60) return formatter.format(seconds, "second");
  const minutes = Math.round(seconds / 60);
  if (Math.abs(minutes) < 60) return formatter.format(minutes, "minute");
  const hours = Math.round(minutes / 60);
  if (Math.abs(hours) < 24) return formatter.format(hours, "hour");
  return formatter.format(Math.round(hours / 24), "day");
}

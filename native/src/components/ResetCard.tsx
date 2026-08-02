import { useState, type FormEvent } from "react";
import { resetDaemon } from "../api";

export function ResetCard({
  activeSessions,
  onReset,
}: {
  activeSessions: number;
  onReset: () => Promise<void>;
}) {
  const [expanded, setExpanded] = useState(false);
  const [apiKey, setApiKey] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [force, setForce] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSubmitting(true);
    setError("");
    try {
      const result = await resetDaemon({ apiKey, force });
      if (!result.ok) throw new Error(result.message || "Reset failed");
      setApiKey("");
      setConfirmation("");
      await onReset();
    } catch (cause) {
      setError(String(cause));
    } finally {
      setSubmitting(false);
    }
  }

  if (!expanded) {
    return (
      <section className="card danger-zone">
        <header className="card__header"><h2>Danger Zone</h2></header>
        <p>Stop the LaunchAgent and remove this Mac's configuration, local data, and parent registration.</p>
        <button className="btn btn--danger danger-zone__button" onClick={() => setExpanded(true)}>
          Reset setup…
        </button>
      </section>
    );
  }

  const canSubmit = apiKey.trim() !== "" && confirmation === "RESET" && (activeSessions === 0 || force);
  return (
    <section className="card danger-zone danger-zone--expanded">
      <header className="card__header"><h2>Reset setup</h2></header>
      <p>This removes the daemon deployment, local configuration and credentials, session state, and the registration on the parent server.</p>
      <form onSubmit={(event) => void submit(event)}>
        {activeSessions > 0 && (
          <label className="danger-zone__force">
            <input type="checkbox" checked={force} onChange={(event) => setForce(event.target.checked)} />
            Terminate {activeSessions} active session{activeSessions === 1 ? "" : "s"}
          </label>
        )}
        <label>AgentAPI key<input type="password" required autoComplete="off" value={apiKey} onChange={(event) => setApiKey(event.target.value)} /></label>
        <label>Type <strong>RESET</strong> to confirm<input required autoComplete="off" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} /></label>
        {error && <p className="setup__error">{error}</p>}
        <div className="danger-zone__actions">
          <button className="btn btn--ghost" type="button" disabled={submitting} onClick={() => setExpanded(false)}>Cancel</button>
          <button className="btn btn--danger" type="submit" disabled={submitting || !canSubmit}>{submitting ? "Resetting…" : "Reset everything"}</button>
        </div>
      </form>
      <p className="setup__hint">The API key is passed only to the uninstall command and is not saved.</p>
    </section>
  );
}

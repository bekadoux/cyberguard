import { useEffect, useState } from 'react';
import { useAuth } from '../auth/use-auth';
import { actionError, localTime } from './workflow-ui';
import { workflowApi, type IncidentComment, type AuditEvent } from './workflow-api';
import type { IncidentDetail } from './model';

export function IncidentDiscussion({
  incident,
  professional,
  onChanged,
}: {
  incident: IncidentDetail;
  professional: boolean;
  onChanged: () => void;
}) {
  const { user, runProtected, pending } = useAuth();
  const [comments, setComments] = useState<IncidentComment[]>([]);
  const [history, setHistory] = useState<AuditEvent[]>([]);
  const [text, setText] = useState('');
  const [visibility, setVisibility] = useState<'public' | 'internal'>('public');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    void runProtected(() =>
      Promise.all([
        workflowApi.comments(incident.id, controller.signal),
        professional
          ? workflowApi.history(incident.id, controller.signal)
          : Promise.resolve([]),
      ]),
    ).then(
      ([items, events]) => {
        if (!controller.signal.aborted) {
          setComments(items);
          setHistory(events);
          setError('');
        }
      },
      (error: unknown) => {
        if (!controller.signal.aborted) setError(actionError(error));
      },
    );
    return () => controller.abort();
  }, [incident.id, professional, runProtected, refresh]);
  const canPublic =
    !professional ||
    user?.role === 'admin' ||
    incident.reporter_id === user?.id ||
    incident.assigned_analyst_id === user?.id;
  const canPost = visibility === 'internal' ? professional : canPublic;
  async function send(e: React.FormEvent) {
    e.preventDefault();
    if (!text.trim() || !canPost) return;
    setBusy(true);
    setError('');
    try {
      await runProtected(() =>
        workflowApi.addComment(incident.id, text.trim(), visibility),
      );
      setText('');
      setRefresh((v) => v + 1);
      onChanged();
    } catch (error) {
      setError(actionError(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      className="panel detail-panel workflow-card"
      aria-labelledby="comments-heading"
    >
      <div className="workflow-row">
        <h2 id="comments-heading">Comments</h2>
        <button
          className="button button-secondary"
          onClick={() => setRefresh((v) => v + 1)}
          type="button"
        >
          Refresh
        </button>
      </div>
      {error && (
        <p className="notice notice-error" role="alert">
          {error}
        </p>
      )}
      {comments.length === 0 ? (
        <p className="muted">No comments yet.</p>
      ) : (
        <ul className="activity-list">
          {comments.map((c) => (
            <li key={c.id}>
              <div className="workflow-row">
                <strong>
                  {c.author_id === user?.id ? 'You' : `User ${c.author_id.slice(0, 8)}`}
                </strong>
                <span className="muted">
                  {c.visibility === 'internal' ? 'Internal · staff only' : 'Public'} ·{' '}
                  {localTime(c.created_at)}
                </span>
              </div>
              <p className="text-preserved">{c.text}</p>
            </li>
          ))}
        </ul>
      )}
      <form className="workflow-form" onSubmit={(e) => void send(e)}>
        <h3>Add comment</h3>
        {professional && (
          <label>
            Visibility
            <select
              value={visibility}
              onChange={(e) => setVisibility(e.target.value as 'public' | 'internal')}
            >
              <option value="public">Public — visible to reporter</option>
              <option value="internal">Internal — staff only</option>
            </select>
          </label>
        )}
        <label>
          Your message
          <textarea
            required
            minLength={1}
            maxLength={5000}
            rows={3}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
        </label>
        {!canPost && (
          <p className="muted">
            Only the assigned analyst or admin can post public comments on this incident.
          </p>
        )}
        {incident.status === 'awaiting_reporter' && !professional && (
          <p className="muted">
            Your public reply will automatically resume investigation.
          </p>
        )}
        <button
          type="submit"
          className="button button-primary"
          disabled={busy || pending || !text.trim() || !canPost}
        >
          Post comment
        </button>
      </form>
      {professional && (
        <div className="workflow-form">
          <h3>Incident history</h3>
          {history.length === 0 ? (
            <p className="muted">No history recorded.</p>
          ) : (
            <ol className="activity-list">
              {history.map((entry) => (
                <li key={entry.id}>
                  <strong>{entry.action.replaceAll('_', ' ')}</strong>{' '}
                  <span className="muted">· {localTime(entry.created_at)}</span>
                  {entry.reason && (
                    <p className="text-preserved">Reason: {entry.reason}</p>
                  )}
                  <p className="muted">
                    Actor: {entry.actor_id?.slice(0, 8) ?? 'System'}
                  </p>
                </li>
              ))}
            </ol>
          )}
        </div>
      )}
    </section>
  );
}

import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { useAuth } from '../auth/use-auth';
import { actionError, localTime } from './workflow-ui';
import { notificationsApi, type Notification } from './workflow-api';
import './workflow.css';

export function NotificationsPage() {
  const { runProtected, pending } = useAuth();
  const [items, setItems] = useState<Notification[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    document.title = 'Notifications · CyberGuard';
    const controller = new AbortController();
    void runProtected(() => notificationsApi.list(controller.signal)).then(
      (data) => {
        if (!controller.signal.aborted) setItems(data);
      },
      (e) => {
        if (!controller.signal.aborted) setError(actionError(e));
      },
    );
    return () => controller.abort();
  }, [runProtected, revision]);
  async function setRead(id: string, read: boolean) {
    setBusy(id);
    setError('');
    try {
      await runProtected(() =>
        read ? notificationsApi.read(id) : notificationsApi.unread(id),
      );
      setRevision((r) => r + 1);
    } catch (e) {
      setError(actionError(e));
    } finally {
      setBusy(null);
    }
  }
  return (
    <section aria-label="Notifications">
      <div className="page-heading">
        <div>
          <span className="eyebrow">WORKSPACE</span>
          <h1>Notifications</h1>
          <p>Recent updates to incidents you can access.</p>
        </div>
        <button
          type="button"
          className="button button-secondary"
          onClick={() => setRevision((r) => r + 1)}
        >
          Refresh
        </button>
      </div>
      {error && (
        <p role="alert" className="notice notice-error">
          {error}
        </p>
      )}
      <div className="panel detail-panel">
        <ul className="activity-list">
          {items.map((item) => (
            <li key={item.id}>
              <div className="workflow-row">
                <div>
                  <strong>{item.event_type.replaceAll('_', ' ')}</strong>
                  {!item.read_at && <span className="unread-indicator">Unread</span>}
                </div>
                <span className="muted">{localTime(item.created_at)}</span>
              </div>
              <p className="text-preserved">{item.message}</p>
              <div className="workflow-row">
                {item.incident_id && (
                  <Link to={`/incidents/${item.incident_id}`}>View incident</Link>
                )}
                <button
                  type="button"
                  className="button button-secondary"
                  disabled={pending || busy !== null}
                  onClick={() => void setRead(item.id, !item.read_at)}
                >
                  {item.read_at ? 'Mark unread' : 'Mark read'}
                </button>
              </div>
            </li>
          ))}
        </ul>
        {items.length === 0 && <p className="muted">No notifications.</p>}
      </div>
    </section>
  );
}

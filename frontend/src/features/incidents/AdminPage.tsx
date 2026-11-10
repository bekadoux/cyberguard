import { useEffect, useState } from 'react';
import { useAuth } from '../auth/use-auth';
import { actionError, localTime } from './workflow-ui';
import { adminApi, type ManagedUser, type IdentityEvent } from './workflow-api';
import './workflow.css';

const roles: ManagedUser['role'][] = ['employee', 'analyst', 'admin'];
export function AdminPage() {
  const { user, pending, runProtected } = useAuth();
  const [users, setUsers] = useState<ManagedUser[]>([]);
  const [audit, setAudit] = useState<IdentityEvent[]>([]);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<ManagedUser['role']>('employee');
  const [reason, setReason] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    document.title = 'Administration · CyberGuard';
    if (user?.role !== 'admin') return;
    const controller = new AbortController();
    void runProtected(() =>
      Promise.all([adminApi.users(controller.signal), adminApi.audit(controller.signal)]),
    ).then(
      ([accounts, events]) => {
        if (!controller.signal.aborted) {
          setUsers(accounts);
          setAudit(events);
        }
      },
      (e) => {
        if (!controller.signal.aborted) setError(actionError(e));
      },
    );
    return () => controller.abort();
  }, [user?.role, runProtected, revision]);
  async function mutate(action: () => Promise<unknown>, confirmation: string) {
    setBusy(true);
    setError('');
    setSuccess('');
    try {
      await runProtected(action);
      setSuccess(confirmation);
      setRevision((r) => r + 1);
    } catch (e) {
      setError(actionError(e));
    } finally {
      setBusy(false);
    }
  }
  if (user?.role !== 'admin') return <p role="alert">Administrator access required.</p>;
  return (
    <section aria-label="Administration">
      <div className="page-heading">
        <div>
          <span className="eyebrow">ADMINISTRATION</span>
          <h1>Users & audit</h1>
          <p>Account changes invalidate the affected user's existing sessions.</p>
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
      {success && (
        <p role="status" className="notice">
          {success}
        </p>
      )}
      <section className="panel detail-panel workflow-card">
        <h2>Create user</h2>
        <form
          className="workflow-form"
          onSubmit={(e) => {
            e.preventDefault();
            void mutate(() => adminApi.create(username, password, role), 'User created.');
            setPassword('');
          }}
        >
          <div className="workflow-pair">
            <label>
              Username
              <input
                required
                minLength={3}
                maxLength={64}
                pattern="[a-z0-9][a-z0-9_.-]{2,63}"
                autoComplete="off"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            </label>
            <label>
              Role
              <select
                value={role}
                onChange={(e) => setRole(e.target.value as ManagedUser['role'])}
              >
                {roles.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <label>
            Initial password
            <input
              type="password"
              required
              minLength={12}
              maxLength={128}
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </label>
          <button
            type="submit"
            className="button button-primary"
            disabled={busy || pending}
          >
            Create account
          </button>
        </form>
      </section>
      <section className="panel detail-panel workflow-card">
        <h2>Manage users</h2>
        <label>
          Reason for account changes (optional)
          <textarea
            rows={2}
            maxLength={1000}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </label>
        <ul className="activity-list">
          {users.map((account) => (
            <li key={account.id}>
              <div className="workflow-row">
                <div>
                  <strong>{account.username}</strong>{' '}
                  <span className="muted">
                    {account.is_blocked ? 'Blocked' : 'Active'} · {account.role}
                  </span>
                </div>
                <span className="metadata-id">{account.id.slice(0, 8)}</span>
              </div>
              <div className="workflow-actions">
                <label>
                  Role
                  <select
                    key={`${account.id}-${account.role}`}
                    aria-label={`Role for ${account.username}`}
                    defaultValue={account.role}
                    disabled={busy || account.id === user.id}
                    onChange={(e) => {
                      const nextRole = e.target.value as ManagedUser['role'];
                      if (window.confirm(`Change role for ${account.username}?`))
                        void mutate(
                          () => adminApi.role(account.id, nextRole, reason),
                          'Role updated.',
                        );
                      else setRevision((r) => r + 1);
                    }}
                  >
                    {roles.map((r) => (
                      <option key={r} value={r}>
                        {r}
                      </option>
                    ))}
                  </select>
                </label>
                <button
                  type="button"
                  className="button button-secondary"
                  disabled={busy || account.id === user.id}
                  onClick={() => {
                    if (
                      window.confirm(
                        `${account.is_blocked ? 'Unblock' : 'Block'} ${account.username}?`,
                      )
                    )
                      void mutate(
                        () => adminApi.block(account.id, !account.is_blocked, reason),
                        'Account status updated.',
                      );
                  }}
                >
                  {account.is_blocked ? 'Unblock' : 'Block'}
                </button>
                <button
                  type="button"
                  className="button button-secondary"
                  disabled={busy}
                  onClick={() => {
                    const next = window.prompt(
                      `New password for ${account.username} (12–128 characters):`,
                    );
                    if (next === null) return;
                    if (next.length < 12 || next.length > 128) {
                      setError('Password must be between 12 and 128 characters.');
                      return;
                    }
                    if (window.confirm(`Reset password for ${account.username}?`))
                      void mutate(
                        () => adminApi.password(account.id, next, reason),
                        'Password reset.',
                      );
                  }}
                >
                  Reset password
                </button>
              </div>
            </li>
          ))}
        </ul>
        {users.length === 0 && <p className="muted">No users loaded.</p>}
      </section>
      <section className="panel detail-panel workflow-card">
        <h2>Administration audit</h2>
        <ul className="activity-list">
          {audit.map((entry) => (
            <li key={entry.id}>
              <strong>{entry.action.replaceAll('_', ' ')}</strong>{' '}
              <span className="muted">· {localTime(entry.created_at)}</span>
              <p className="muted">
                Actor {entry.actor_id.slice(0, 8)} · Target {entry.subject_id.slice(0, 8)}
              </p>
            </li>
          ))}
        </ul>
        {audit.length === 0 && <p className="muted">No audit events recorded.</p>}
      </section>
    </section>
  );
}

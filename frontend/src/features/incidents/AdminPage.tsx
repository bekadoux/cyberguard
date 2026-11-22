import { useEffect, useId, useRef, useState } from 'react';
import { useAuth } from '../auth/use-auth';
import { DemoIncidentsPanel } from './DemoIncidentsPanel';
import { actionError, localTime } from './workflow-ui';
import { adminApi, type ManagedUser, type IdentityEvent } from './workflow-api';
import './workflow.css';

const roles: ManagedUser['role'][] = ['employee', 'analyst', 'admin'];
const passwordHelp =
  'Use 12–128 UTF-8 bytes, including a non-whitespace character and no NUL characters.';

function NewPasswordField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (password: string) => void;
}) {
  const hintId = useId();
  return (
    <div>
      <label>
        {label}
        <input
          type="password"
          required
          autoComplete="new-password"
          aria-describedby={hintId}
          value={value}
          onChange={({ currentTarget }) => {
            const next = currentTarget.value;
            const bytes = new TextEncoder().encode(next).length;
            const valid =
              bytes >= 12 &&
              bytes <= 128 &&
              /\P{White_Space}/u.test(next) &&
              !next.includes('\0');
            currentTarget.setCustomValidity(valid ? '' : passwordHelp);
            onChange(next);
          }}
        />
      </label>
      <small id={hintId}>{passwordHelp}</small>
    </div>
  );
}

function ResetPasswordForm({
  username,
  confirmCurrentPassword,
  disabled,
  onSubmit,
  onCancel,
}: {
  username: string;
  confirmCurrentPassword: boolean;
  disabled: boolean;
  onSubmit: (password: string, currentPassword?: string) => void;
  onCancel: () => void;
}) {
  const [password, setPassword] = useState('');
  const [currentPassword, setCurrentPassword] = useState('');
  return (
    <form
      className="workflow-form"
      aria-label={`Reset password for ${username}`}
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(password, confirmCurrentPassword ? currentPassword : undefined);
      }}
    >
      {confirmCurrentPassword && (
        <label>
          Current password for {username}
          <input
            type="password"
            autoComplete="current-password"
            required
            value={currentPassword}
            onChange={({ currentTarget }) => {
              const next = currentTarget.value;
              currentTarget.setCustomValidity(
                new TextEncoder().encode(next).length <= 128
                  ? ''
                  : 'Use at most 128 UTF-8 bytes.',
              );
              setCurrentPassword(next);
            }}
          />
        </label>
      )}
      <NewPasswordField
        label={`New password for ${username}`}
        value={password}
        onChange={setPassword}
      />
      <div className="workflow-actions">
        <button type="submit" className="button button-primary" disabled={disabled}>
          Confirm password reset
        </button>
        <button type="button" className="button button-secondary" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}

export function AdminPage() {
  const { user, pending, runProtected } = useAuth();
  const [users, setUsers] = useState<ManagedUser[]>([]);
  const [audit, setAudit] = useState<IdentityEvent[]>([]);
  const [ownerId, setOwnerId] = useState<string | null>();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [resetId, setResetId] = useState<string | null>(null);
  const resetOpener = useRef<HTMLButtonElement | null>(null);
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
      Promise.all([
        adminApi.users(controller.signal),
        adminApi.audit(controller.signal),
        adminApi.ownerId(controller.signal),
      ]),
    ).then(
      ([accounts, events, owner]) => {
        if (!controller.signal.aborted) {
          setUsers(accounts);
          setAudit(events);
          setOwnerId(owner);
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
      <DemoIncidentsPanel key={user.id} />
      <section className="panel detail-panel workflow-card">
        <h2>Create user</h2>
        <p>New accounts are employees. To assign another role, use Manage users below.</p>
        <form
          className="workflow-form"
          onSubmit={(e) => {
            e.preventDefault();
            void mutate(() => adminApi.create(username, password), 'User created.');
            setPassword('');
          }}
        >
          <label>
            Username
            <input
              required
              minLength={3}
              maxLength={64}
              pattern="[a-z0-9][a-z0-9_.\-]{2,63}"
              autoComplete="off"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </label>
          <NewPasswordField
            label="Initial password"
            value={password}
            onChange={setPassword}
          />
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
          {users.map((account) => {
            const isOwner = account.id === ownerId;
            const isSelf = account.id === user.id;
            const mayReset = ownerId !== undefined && (!isOwner || isSelf);
            return (
              <li key={account.id}>
                <div className="workflow-row">
                  <div>
                    <strong>{account.username}</strong>{' '}
                    <span className="muted">
                      {account.is_blocked ? 'Blocked' : 'Active'} · {account.role}
                      {isOwner && ' · Application owner'}
                    </span>
                  </div>
                  <span className="metadata-id">{account.id.slice(0, 8)}</span>
                </div>
                <div className="workflow-actions">
                  <label>
                    Role
                    <select
                      aria-label={`Role for ${account.username}`}
                      value={account.role}
                      disabled={busy || isSelf || isOwner}
                      onChange={(e) => {
                        const nextRole = e.target.value as ManagedUser['role'];
                        if (window.confirm(`Change role for ${account.username}?`))
                          void mutate(
                            () => adminApi.role(account.id, nextRole, reason),
                            'Role updated.',
                          );
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
                    disabled={busy || isSelf || isOwner}
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
                    aria-label={`Reset password for ${account.username}`}
                    aria-expanded={resetId === account.id}
                    disabled={busy || pending || !mayReset}
                    onClick={(e) => {
                      resetOpener.current = e.currentTarget;
                      setResetId(resetId === account.id ? null : account.id);
                    }}
                  >
                    Reset password
                  </button>
                </div>
                {resetId === account.id && mayReset && (
                  <ResetPasswordForm
                    key={String(isOwner)}
                    username={account.username}
                    confirmCurrentPassword={isOwner && isSelf}
                    disabled={busy || pending}
                    onCancel={() => {
                      setResetId(null);
                      resetOpener.current?.focus();
                    }}
                    onSubmit={(next, current) => {
                      setResetId(null);
                      void mutate(
                        () => adminApi.password(account.id, next, reason, current),
                        'Password reset.',
                      );
                    }}
                  />
                )}
              </li>
            );
          })}
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

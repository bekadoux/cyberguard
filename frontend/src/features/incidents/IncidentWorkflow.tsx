import { useEffect, useState } from 'react';
import { useAuth } from '../auth/use-auth';
import type {
  IncidentCategory,
  IncidentDetail,
  IncidentSeverity,
  IncidentStatus,
} from './model';
import { categoryLabels, severityLabels } from './model';
import { adminApi, workflowApi, type ManagedUser } from './workflow-api';
import { actionError, availableTransitions, terminal } from './workflow-ui';

const labels: Record<string, string> = {
  investigating: 'Start / reopen investigation',
  false_positive: 'Mark as false positive',
  awaiting_reporter: 'Request reporter reply',
  resolved: 'Mark resolved',
  closed: 'Confirm and close',
};

export function IncidentWorkflow({
  incident,
  onChanged,
}: {
  incident: IncidentDetail;
  onChanged: () => void;
}) {
  const { user, pending, runProtected } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [category, setCategory] = useState<IncidentCategory | ''>(
    incident.category ?? '',
  );
  const [severity, setSeverity] = useState<IncidentSeverity | ''>(
    incident.severity ?? '',
  );
  const [system, setSystem] = useState(incident.affected_system ?? '');
  const [resolution, setResolution] = useState(incident.resolution_note ?? '');
  const [status, setStatus] = useState<IncidentStatus | ''>('');
  const [reason, setReason] = useState('');
  const [archiveReason, setArchiveReason] = useState('');
  const [analystId, setAnalystId] = useState('');
  const [analysts, setAnalysts] = useState<ManagedUser[]>([]);
  const canEdit =
    user?.role === 'admin' ||
    (user?.role === 'analyst' && incident.assigned_analyst_id === user.id);
  const canClaim =
    (user?.role === 'analyst' || user?.role === 'admin') &&
    incident.status === 'submitted' &&
    !incident.assigned_analyst_id;
  const transitions = availableTransitions(incident, user?.role, user?.id);
  useEffect(() => {
    if (user?.role !== 'admin') return;
    const controller = new AbortController();
    void runProtected(() => adminApi.users(controller.signal)).then(
      (users) => {
        if (!controller.signal.aborted)
          setAnalysts(users.filter((u) => u.role === 'analyst' && !u.is_blocked));
      },
      () => {
        if (!controller.signal.aborted)
          setError('Unable to load analysts for assignment.');
      },
    );
    return () => controller.abort();
  }, [user?.role, runProtected]);
  async function submit(
    action: () => Promise<unknown>,
    message: string,
    archived = false,
  ) {
    setBusy(true);
    setError('');
    setSuccess('');
    try {
      await runProtected(action);
      setSuccess(message);
      if (archived) window.location.assign('/incidents');
      else onChanged();
    } catch (e) {
      setError(actionError(e));
    } finally {
      setBusy(false);
    }
  }
  const disabled = busy || pending;
  const needsReason =
    status === 'false_positive' ||
    (status === 'investigating' &&
      (incident.status === 'resolved' || terminal.includes(incident.status)));
  return (
    <section
      className="panel detail-panel workflow-card"
      aria-labelledby="workflow-heading"
    >
      <h2 id="workflow-heading">Investigation actions</h2>
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
      {canClaim && (
        <div className="workflow-row">
          <p>Unassigned report — you can claim it for triage.</p>
          <button
            type="button"
            className="button button-primary"
            disabled={disabled}
            onClick={() =>
              void submit(() => workflowApi.claim(incident.id), 'Incident claimed.')
            }
          >
            Claim incident
          </button>
        </div>
      )}
      {user?.role === 'admin' && !['resolved', ...terminal].includes(incident.status) && (
        <form
          className="workflow-form"
          onSubmit={(e) => {
            e.preventDefault();
            if (analystId)
              void submit(
                () => workflowApi.assign(incident.id, analystId),
                'Assignment updated.',
              );
          }}
        >
          <h3>Assign / reassign</h3>
          <label>
            Active analyst
            <select
              value={analystId}
              required
              onChange={(e) => setAnalystId(e.target.value)}
            >
              <option value="">Select analyst</option>
              {analysts.map((a) => (
                <option value={a.id} key={a.id}>
                  {a.username}
                </option>
              ))}
            </select>
          </label>
          <button
            className="button button-secondary"
            type="submit"
            disabled={disabled || !analystId}
          >
            Save assignment
          </button>
        </form>
      )}
      {canEdit && !terminal.includes(incident.status) && (
        <form
          className="workflow-form"
          onSubmit={(e) => {
            e.preventDefault();
            void submit(
              () =>
                workflowApi.fields(incident.id, {
                  category: category || null,
                  severity: severity || null,
                  affected_system: system.trim() || null,
                  resolution_note: resolution.trim() || null,
                }),
              'Incident fields saved.',
            );
          }}
        >
          <h3>Edit confirmed fields</h3>
          <p className="muted">
            Category and severity are human decisions. The original report cannot be
            edited.
          </p>
          <div className="workflow-pair">
            <label>
              Category
              <select
                value={category}
                onChange={(e) => setCategory(e.target.value as typeof category)}
              >
                <option value="">Not assessed</option>
                {Object.entries(categoryLabels).map(([value, label]) => (
                  <option value={value} key={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Severity
              <select
                value={severity}
                onChange={(e) => setSeverity(e.target.value as typeof severity)}
              >
                <option value="">Not assessed</option>
                {Object.entries(severityLabels).map(([value, label]) => (
                  <option value={value} key={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <label>
            Affected system
            <input
              maxLength={500}
              value={system}
              onChange={(e) => setSystem(e.target.value)}
            />
          </label>
          <label>
            Resolution note
            <textarea
              rows={3}
              maxLength={10000}
              value={resolution}
              onChange={(e) => setResolution(e.target.value)}
            />
          </label>
          <button className="button button-secondary" type="submit" disabled={disabled}>
            Save fields
          </button>
        </form>
      )}
      {transitions.length > 0 && (
        <form
          className="workflow-form"
          onSubmit={(e) => {
            e.preventDefault();
            if (status && (!needsReason || reason.trim()))
              void submit(
                () => workflowApi.transition(incident.id, status, reason.trim()),
                'Incident status updated.',
              );
          }}
        >
          <h3>Change status</h3>
          <label>
            Next action
            <select
              required
              value={status}
              onChange={(e) => {
                setStatus(e.target.value as IncidentStatus | '');
                setReason('');
              }}
            >
              <option value="">Select action</option>
              {transitions.map((s) => (
                <option value={s} key={s}>
                  {labels[s]}
                </option>
              ))}
            </select>
          </label>
          {status === 'awaiting_reporter' && (
            <p className="muted">
              Post a public question in Comments before requesting a reporter reply.
            </p>
          )}
          {status === 'investigating' && incident.status === 'triage' && (
            <p className="muted">
              Category and severity must be saved before starting investigation.
            </p>
          )}
          {status === 'resolved' && (
            <p className="muted">Save a resolution note before resolving the incident.</p>
          )}
          <label>
            Reason {needsReason ? '(required)' : '(optional)'}
            <textarea
              maxLength={2000}
              rows={2}
              required={needsReason}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
          </label>
          <button
            className="button button-primary"
            type="submit"
            disabled={disabled || !status || (needsReason && !reason.trim())}
          >
            Apply transition
          </button>
        </form>
      )}
      {user?.role === 'admin' && (
        <form
          className="workflow-form"
          onSubmit={(e) => {
            e.preventDefault();
            if (
              archiveReason.trim() &&
              window.confirm(
                'Archive this incident? It will disappear from active incident lists.',
              )
            )
              void submit(
                () => workflowApi.archive(incident.id, archiveReason.trim()),
                'Incident archived.',
                true,
              );
          }}
        >
          <h3>Archive incident</h3>
          <label>
            Archive reason
            <textarea
              required
              maxLength={2000}
              rows={2}
              value={archiveReason}
              onChange={(e) => setArchiveReason(e.target.value)}
            />
          </label>
          <button
            className="button button-secondary"
            type="submit"
            disabled={disabled || !archiveReason.trim()}
          >
            Archive
          </button>
        </form>
      )}
    </section>
  );
}

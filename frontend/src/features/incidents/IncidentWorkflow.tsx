import { useState } from 'react';
import { useAuth } from '../auth/use-auth';
import type { IncidentDetail, IncidentStatus } from './model';
import { AssignmentPicker } from './AssignmentPicker';
import { ConfirmedFields } from './ConfirmedFields';
import { workflowApi, type IncidentFields } from './workflow-api';
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
  onChanged: (saved?: IncidentFields) => void;
}) {
  const { user, pending, runProtected } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState('');
  const [status, setStatus] = useState<IncidentStatus | ''>('');
  const [reason, setReason] = useState('');
  const [archiveReason, setArchiveReason] = useState('');
  const canEdit =
    user?.role === 'admin' ||
    (user?.role === 'analyst' && incident.assigned_analyst_id === user.id);
  const canClaim =
    (user?.role === 'analyst' || user?.role === 'admin') &&
    incident.status === 'submitted' &&
    !incident.assigned_analyst_id;
  const transitions = availableTransitions(incident, user?.role, user?.id);
  async function submit(
    action: () => Promise<unknown>,
    message: string,
    afterSave: () => void = onChanged,
  ) {
    setBusy(true);
    setError('');
    setSuccess('');
    try {
      await runProtected(action);
      setSuccess(message);
      afterSave();
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
        <AssignmentPicker
          disabled={disabled}
          currentId={incident.assigned_analyst_id}
          assign={(id) =>
            void submit(() => workflowApi.assign(incident.id, id), 'Assignment updated.')
          }
        />
      )}
      {canEdit && !terminal.includes(incident.status) && (
        <ConfirmedFields
          incident={incident}
          disabled={disabled}
          save={(patch) => {
            void submit(
              () => workflowApi.fields(incident.id, patch),
              'Incident fields saved.',
              () => onChanged(patch),
            );
          }}
        />
      )}
      {transitions.length > 0 && (
        <form
          className="workflow-form"
          onSubmit={(e) => {
            e.preventDefault();
            if (status && transitions.includes(status) && (!needsReason || reason.trim()))
              void submit(
                () => workflowApi.transition(incident.id, status, reason.trim()),
                'Incident status updated.',
                () => {
                  setStatus('');
                  setReason('');
                  onChanged();
                },
              );
          }}
        >
          <h3>Change status</h3>
          <label>
            Next action
            <select
              required
              disabled={disabled}
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
              disabled={disabled}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
          </label>
          <button
            className="button button-primary"
            type="submit"
            disabled={
              disabled ||
              !status ||
              !transitions.includes(status) ||
              (needsReason && !reason.trim())
            }
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
                () => window.location.assign('/incidents'),
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

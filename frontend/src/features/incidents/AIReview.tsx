import { useState } from 'react';
import { useAuth } from '../auth/use-auth';
import type { Assessment } from './assessment';
import { categoryLabels, type IncidentCategory, type IncidentDetail } from './model';
import { workflowApi, type FeedbackDecision } from './workflow-api';
import { actionError } from './workflow-ui';

export function AIReview({
  assessment,
  incident,
  onChanged,
}: {
  assessment: Assessment;
  incident: IncidentDetail;
  onChanged: () => void;
}) {
  const { user, pending, runProtected } = useAuth();
  const [decision, setDecision] = useState<FeedbackDecision>(
    assessment.result?.assessment.triage.suggested_category ? 'accepted' : 'corrected',
  );
  const [category, setCategory] = useState<IncidentCategory | ''>(
    assessment.result?.assessment.triage.suggested_category ?? '',
  );
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState('');
  const suggested = assessment.result?.assessment.triage.suggested_category;
  const canReview =
    user?.role === 'admin' ||
    (user?.role === 'analyst' && incident.assigned_analyst_id === user.id);
  if (
    !assessment.result ||
    assessment.result.source_kind !== 'kev_llm' ||
    !canReview ||
    incident.status === 'closed' ||
    incident.status === 'false_positive'
  )
    return null;
  const invalid =
    (decision === 'accepted' && (!suggested || category !== suggested)) ||
    (decision === 'corrected' &&
      (!category || category === suggested || !reason.trim())) ||
    (decision === 'rejected' && !reason.trim());
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (invalid) return;
    setBusy(true);
    setError('');
    try {
      await runProtected(() =>
        workflowApi.feedback(incident.id, {
          job_id: assessment.job.id,
          decision,
          category: decision === 'rejected' ? null : category || null,
          reason: reason.trim(),
        }),
      );
      setSaved(true);
      onChanged();
    } catch (e) {
      setError(actionError(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form
      className="workflow-form"
      onSubmit={(e) => void submit(e)}
      aria-label="AI feedback review"
    >
      <h3>Human review & AI feedback</h3>
      <p className="muted">
        You are accountable for the confirmed category. Saving a decision records an
        auditable review; only accepted/corrected decisions update the incident category.
        One decision is allowed per assessment.
      </p>
      {error && (
        <p className="notice notice-error" role="alert">
          {error}
        </p>
      )}
      {saved ? (
        <p role="status">AI review recorded successfully. See incident history.</p>
      ) : (
        <>
          <label>
            Decision
            <select
              value={decision}
              onChange={(e) => {
                const value = e.target.value as FeedbackDecision;
                setDecision(value);
                if (value === 'accepted') setCategory(suggested ?? '');
              }}
            >
              <option value="accepted" disabled={!suggested}>
                Accept suggested category
              </option>
              <option value="corrected">Correct suggested category</option>
              <option value="rejected">Reject assessment</option>
            </select>
          </label>
          {decision !== 'rejected' && (
            <label>
              Confirmed category
              <select
                required
                value={category}
                disabled={decision === 'accepted'}
                onChange={(e) => setCategory(e.target.value as IncidentCategory | '')}
              >
                <option value="">Select category</option>
                {Object.entries(categoryLabels).map(([key, label]) => (
                  <option key={key} value={key}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
          )}
          <label>
            Reason {decision === 'accepted' ? '(optional)' : '(required)'}
            <textarea
              rows={2}
              maxLength={2000}
              required={decision !== 'accepted'}
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
          </label>
          <button
            type="submit"
            className="button button-primary"
            disabled={busy || pending || invalid}
          >
            Save human decision
          </button>
        </>
      )}
    </form>
  );
}

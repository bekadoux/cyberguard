import { useEffect, useState } from 'react';
import { ApiError } from '../../shared/api/client';
import { useAuth } from '../auth/use-auth';
import type { Assessment } from './assessment';
import { categoryLabels, type IncidentCategory, type IncidentDetail } from './model';
import { workflowApi, type FeedbackDecision, type JobReview } from './workflow-api';
import { actionError, terminal } from './workflow-ui';

type Props = {
  assessment: Assessment;
  incident: IncidentDetail;
  version: string;
  onChanged: () => void;
};

export function AIReview(props: Props) {
  const { user } = useAuth();
  if (
    props.assessment.result?.source_kind !== 'kev_llm' ||
    (user?.role !== 'admin' && user?.role !== 'analyst')
  )
    return null;
  return (
    <JobReviewPanel
      key={`${user.id}/${user.role}/${props.incident.id}/${props.assessment.job.id}`}
      {...props}
    />
  );
}

function JobReviewPanel({ assessment, incident, version, onChanged }: Props) {
  const { user, pending, runProtected } = useAuth();
  const [decision, setDecision] = useState<FeedbackDecision>(
    assessment.result?.assessment.triage.suggested_category ? 'accepted' : 'corrected',
  );
  const [category, setCategory] = useState<IncidentCategory | ''>(
    assessment.result?.assessment.triage.suggested_category ?? '',
  );
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const source = `${version}/${revision}`;
  const [read, setRead] = useState<{
    source: string;
    review: JobReview | null;
    error?: string;
  } | null>(null);
  const jobId = assessment.job.id;
  useEffect(() => {
    const controller = new AbortController();
    void runProtected(() =>
      workflowApi.review(incident.id, jobId, controller.signal),
    ).then(
      (review) => {
        if (!controller.signal.aborted) setRead({ source, review });
      },
      (err: unknown) => {
        if (!controller.signal.aborted)
          setRead({ source, review: null, error: actionError(err) });
      },
    );
    return () => controller.abort();
  }, [incident.id, jobId, source, runProtected]);
  const [error, setError] = useState('');
  const suggested = assessment.result?.assessment.triage.suggested_category;
  if (read?.source !== source) return <p role="status">Loading human review…</p>;
  if (read.error)
    return (
      <div className="workflow-form">
        <p role="alert">Unable to load human review. {read.error}</p>
        <button
          className="button button-secondary"
          onClick={() => setRevision((v) => v + 1)}
        >
          Retry review
        </button>
      </div>
    );
  if (read.review) {
    const { decision, category, reason } = read.review;
    return (
      <section className="workflow-form" aria-label="Recorded human review">
        <h3>Recorded human review</h3>
        <p>
          Decision: <strong>{decision}</strong>
        </p>
        {category && <p>Confirmed category: {categoryLabels[category]}</p>}
        {reason && <p className="text-preserved">Reason: {reason}</p>}
      </section>
    );
  }
  const canSubmit =
    user?.role === 'admin' ||
    (user?.role === 'analyst' && incident.assigned_analyst_id === user.id);
  if (!canSubmit || terminal.includes(incident.status)) return null;
  const invalid =
    (decision === 'accepted' && (!suggested || category !== suggested)) ||
    (decision === 'corrected' &&
      (!category || category === suggested || !reason.trim())) ||
    (decision === 'rejected' && !reason.trim());
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (invalid || busy || pending) return;
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
      setRevision((v) => v + 1);
      onChanged();
    } catch (e) {
      setError(actionError(e));
      if (e instanceof ApiError && [403, 404, 409].includes(e.status)) {
        setRevision((v) => v + 1);
        onChanged();
      }
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
      <label>
        Decision
        <select
          value={decision}
          disabled={busy}
          onChange={(e) => {
            const value = e.target.value as FeedbackDecision;
            setDecision(value);
            if (value === 'accepted') setCategory(suggested ?? '');
          }}
        >
          <option value="accepted" disabled={!suggested}>
            Accept suggested category
          </option>
          <option value="corrected">
            {suggested ? 'Correct suggested category' : 'Choose a category'}
          </option>
          <option value="rejected">Reject assessment</option>
        </select>
      </label>
      {decision === 'accepted' && suggested && (
        <p className="review-accepted-category">
          Category to confirm: <strong>{categoryLabels[suggested]}</strong>
        </p>
      )}
      {decision === 'corrected' && (
        <label>
          {suggested ? 'Corrected category' : 'Chosen category'}
          <select
            required
            value={category}
            disabled={busy}
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
          disabled={busy}
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
    </form>
  );
}

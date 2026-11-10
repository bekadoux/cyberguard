import { RefreshCw } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useAuth } from '../auth/use-auth';
import {
  assessmentError,
  assessmentFailure,
  getAssessment,
  indicatorLabels,
  jobLabels,
  type Assessment,
} from './assessment';
import { isAborted } from './messages';
import { categoryLabels } from './model';

type State = {
  source: string;
  data: Assessment | null;
  error: unknown;
  loading: boolean;
};
const initial: State = { source: '', data: null, error: null, loading: true };

/** Manual refresh deliberately avoids a polling loop that would keep sessions alive. */
export function AssessmentPanel({ incidentId }: { incidentId: string }) {
  const { runProtected, pending } = useAuth();
  const [reload, setReload] = useState(0);
  const [state, setState] = useState<State>(initial);
  const source = `${incidentId}/${reload}`;
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      setState({ ...initial, source });
      if (pending) return;
      try {
        const data = await runProtected(() =>
          getAssessment(incidentId, controller.signal),
        );
        if (!controller.signal.aborted)
          setState({ source, data, error: null, loading: false });
      } catch (error) {
        if (!controller.signal.aborted && !isAborted(error))
          setState({ source, data: null, error, loading: false });
      }
    }
    void load();
    return () => controller.abort();
  }, [incidentId, source, pending, runProtected]);
  const current = state.source === source ? state : initial;
  const data = current.data;
  const result = data?.result;
  const brief = result?.assessment.brief;
  return (
    <section
      className="panel detail-panel assessment-panel"
      aria-labelledby="assessment-heading"
      data-testid="assessment-panel"
    >
      <div className="assessment-heading">
        <h2 id="assessment-heading">AI assessment</h2>
        <button
          className="button button-secondary"
          type="button"
          disabled={current.loading || pending}
          onClick={() => setReload((value) => value + 1)}
        >
          <RefreshCw size={15} aria-hidden="true" />
          Refresh assessment
        </button>
      </div>
      <p className="assessment-advisory">
        Unconfirmed working hypotheses for security staff, not verified evidence.
        Suggestions do not change the incident’s category, severity or status.
      </p>
      {current.loading ? (
        <p role="status">Loading assessment…</p>
      ) : current.error !== null ? (
        <p role="status">{assessmentError(current.error)}</p>
      ) : (
        data && (
          <>
            <div className="assessment-state">
              <span className={`incident-status assessment-${data.job.status}`}>
                {jobLabels[data.job.status]}
              </span>
              <span>
                Attempt {data.job.attempt_count} · Report revision{' '}
                {data.job.incident_revision}
              </span>
            </div>
            {data.job.status === 'queued' && (
              <p>
                Waiting for a worker. Processing may not be enabled. Refresh to check for
                an update.
              </p>
            )}
            {data.job.status === 'running' && (
              <p>
                Assessment is processing. Refresh to check for an update; no completion
                time is promised.
              </p>
            )}
            {data.job.status === 'failed' && (
              <p className="notice notice-error">
                {assessmentFailure(data.job.error_code)} This screen cannot restart
                processing.
              </p>
            )}
            {result && brief && (
              <>
                {result.source_kind === 'fake' && (
                  <p className="notice assessment-fixture" role="note">
                    <strong>Test fixture — not AI output.</strong> Synthetic integration
                    data must not be used for security decisions.
                  </p>
                )}
                <h3>Draft summary</h3>
                <p className="text-preserved">{brief.summary}</p>
                {(
                  [
                    ['Reported facts (model-extracted)', brief.reported_facts],
                    ['Unknowns', brief.unknowns],
                    ['Follow-up questions', brief.follow_up_questions],
                    ['Suggested playbook steps', brief.suggested_steps],
                  ] as const
                ).map(([heading, items]) => (
                  <div key={heading}>
                    <h3>{heading}</h3>
                    {items.length ? (
                      <ul>
                        {items.map((item, index) => (
                          <li className="text-preserved" key={index}>
                            {item}
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="muted">None supplied.</p>
                    )}
                  </div>
                ))}
                <h3>Unconfirmed model suggestions</h3>
                <dl className="assessment-signals">
                  <div>
                    <dt>Suggested category</dt>
                    <dd>
                      {result.assessment.triage.suggested_category
                        ? categoryLabels[result.assessment.triage.suggested_category]
                        : 'No category suggested'}
                    </dd>
                  </div>
                  <div>
                    <dt>Model impact (0–3)</dt>
                    <dd>{result.assessment.triage.potential_impact ?? 'Not supplied'}</dd>
                  </div>
                  <div>
                    <dt>Model</dt>
                    <dd className="text-preserved">{result.assessment.triage.model}</dd>
                  </div>
                </dl>
                <details>
                  <summary>Model signals</summary>
                  <p>
                    Probabilities are not calibrated risk scores. Analyst review is
                    required.
                  </p>
                  <dl className="assessment-signals">
                    {Object.entries(result.assessment.triage.category_probabilities).map(
                      ([key, value]) => (
                        <div key={key}>
                          <dt>{categoryLabels[key as keyof typeof categoryLabels]}</dt>
                          <dd>{(value * 100).toFixed(1)}%</dd>
                        </div>
                      ),
                    )}
                    {Object.entries(result.assessment.triage.indicator_probabilities).map(
                      ([key, value]) => (
                        <div key={key}>
                          <dt>{indicatorLabels[key as keyof typeof indicatorLabels]}</dt>
                          <dd>{(value * 100).toFixed(1)}%</dd>
                        </div>
                      ),
                    )}
                  </dl>
                </details>
                <p className="text-preserved assessment-disclaimer">
                  {result.assessment.disclaimer}
                </p>
                <p className="assessment-time">
                  Saved{' '}
                  <time dateTime={result.created_at}>
                    {new Date(result.created_at).toLocaleString()}
                  </time>{' '}
                  · {result.source_kind === 'fake' ? 'Synthetic fixture' : 'Kev / LLM'}
                </p>
              </>
            )}
          </>
        )
      )}
    </section>
  );
}

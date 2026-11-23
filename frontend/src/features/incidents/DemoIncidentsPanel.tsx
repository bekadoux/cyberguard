import { useEffect, useState } from 'react';
import { ApiError } from '../../shared/api/client';
import { useAuth } from '../auth/use-auth';
import { adminApi, demoIncidentBatchLimit } from './workflow-api';
import { actionError } from './workflow-ui';

export function DemoIncidentsPanel() {
  const { pending, runProtected } = useAuth();
  const [remaining, setRemaining] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [created, setCreated] = useState(0);
  const batchSize = Math.min(remaining ?? 0, demoIncidentBatchLimit);

  useEffect(() => {
    const controller = new AbortController();
    void runProtected(() => adminApi.demoIncidentStatus(controller.signal)).then(
      (result) => {
        if (!controller.signal.aborted)
          setRemaining(result.enabled ? result.remaining : null);
      },
      () => {
        if (!controller.signal.aborted)
          setError('Could not check demo incident availability. Reload to try again.');
      },
    );
    return () => controller.abort();
  }, [runProtected]);

  async function addDemoIncidents() {
    if (
      !window.confirm(
        `Add up to ${batchSize} synthetic ${batchSize === 1 ? 'incident' : 'incidents'} and queue their AI assessments?`,
      )
    )
      return;
    setBusy(true);
    setError('');
    setCreated(0);
    try {
      const result = await runProtected(adminApi.addDemoIncidents);
      setCreated(result.created);
      setRemaining(result.remaining);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) setRemaining(null);
      setError(
        e instanceof ApiError && e.code === 'demo_accounts_unavailable'
          ? 'Provision or restore the active demo accounts employee_a, employee_b, and analyst first.'
          : actionError(e),
      );
    } finally {
      setBusy(false);
    }
  }

  if (remaining === null && !error) return null;
  return (
    <section className="panel detail-panel workflow-card">
      <h2>Demo data</h2>
      {error && (
        <p role="alert" className="notice notice-error">
          {error}
        </p>
      )}
      {created > 0 && (
        <p role="status" className="notice">
          {created} demo {created === 1 ? 'incident' : 'incidents'} added and queued for
          AI assessment.
        </p>
      )}
      {remaining === 0 && <p>No unused demo scenarios remain in this database.</p>}
      {batchSize > 0 && (
        <>
          <p>Add unused synthetic reports from employee_a, employee_b, and analyst.</p>
          <button
            type="button"
            className="button button-primary"
            disabled={busy || pending}
            onClick={() => void addDemoIncidents()}
          >
            Add {batchSize} demo {batchSize === 1 ? 'incident' : 'incidents'}
          </button>
        </>
      )}
    </section>
  );
}

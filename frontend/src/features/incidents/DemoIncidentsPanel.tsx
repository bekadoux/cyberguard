import { useEffect, useState } from 'react';
import { ApiError } from '../../shared/api/client';
import { useAuth } from '../auth/use-auth';
import { adminApi } from './workflow-api';
import { actionError } from './workflow-ui';

export function DemoIncidentsPanel() {
  const { pending, runProtected } = useAuth();
  const [status, setStatus] = useState<'ready' | 'seeded' | 'disabled'>('disabled');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [created, setCreated] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    void runProtected(() => adminApi.demoIncidentStatus(controller.signal)).then(
      (result) => {
        if (!controller.signal.aborted) setStatus(result);
      },
      () => {
        if (!controller.signal.aborted)
          setError('Could not check demo incident availability. Reload to try again.');
      },
    );
    return () => controller.abort();
  }, [runProtected]);

  async function addDemoIncidents() {
    if (!window.confirm('Add 10 synthetic incidents and queue 10 AI assessments?'))
      return;
    setBusy(true);
    setError('');
    try {
      const count = await runProtected(adminApi.addDemoIncidents);
      setCreated(count === 10);
      setStatus('seeded');
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) setStatus('disabled');
      setError(
        e instanceof ApiError && e.code === 'demo_accounts_unavailable'
          ? 'Provision or restore the active demo accounts employee_a, employee_b, and analyst first.'
          : actionError(e),
      );
    } finally {
      setBusy(false);
    }
  }

  if (status === 'disabled' && !error) return null;
  return (
    <section className="panel detail-panel workflow-card">
      <h2>Demo data</h2>
      {error && (
        <p role="alert" className="notice notice-error">
          {error}
        </p>
      )}
      {status === 'ready' && (
        <>
          <p>
            Create one batch of synthetic reports from employee_a, employee_b, and
            analyst.
          </p>
          <button
            type="button"
            className="button button-primary"
            disabled={busy || pending}
            onClick={() => void addDemoIncidents()}
          >
            Add 10 demo incidents
          </button>
        </>
      )}
      {status === 'seeded' &&
        (created ? (
          <p role="status" className="notice">
            10 demo incidents added and queued for AI assessment.
          </p>
        ) : (
          <p>Demo incidents have already been added to this database.</p>
        ))}
    </section>
  );
}

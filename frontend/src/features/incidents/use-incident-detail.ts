import { useEffect, useState } from 'react';
import { ApiError } from '../../shared/api/client';
import { useAuth } from '../auth/use-auth';
import { incidentsApi } from './api';
import { isAborted } from './messages';
import type { IncidentDetail } from './model';
import type { IncidentFields } from './workflow-api';

type DetailState = {
  incident: IncidentDetail | null;
  error: Error | null;
};
const initial: DetailState = { incident: null, error: null };

export function useIncidentDetail(id: string) {
  const { runProtected, pending } = useAuth();
  const [state, setState] = useState<DetailState>(initial);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      if (pending) return;
      try {
        const incident = await runProtected(() =>
          incidentsApi.get(id, controller.signal),
        );
        if (!controller.signal.aborted) setState({ incident, error: null });
      } catch (error) {
        if (!controller.signal.aborted && !isAborted(error))
          setState((previous) => ({
            incident:
              error instanceof ApiError && (error.status === 0 || error.status >= 500)
                ? previous.incident
                : null,
            error: error instanceof Error ? error : new Error('Incident read failed'),
          }));
      }
    }
    void load();
    return () => controller.abort();
  }, [id, reload, pending, runProtected]);
  return {
    ...state,
    loading: pending || (!state.incident && !state.error),
    revision: reload,
    retry: () => setReload((value) => value + 1),
    changed: (saved?: IncidentFields) => {
      if (saved)
        setState((previous) => ({
          ...previous,
          incident: previous.incident && { ...previous.incident, ...saved },
        }));
      setReload((value) => value + 1);
    },
  };
}

import { useEffect, useState } from 'react';
import { useAuth } from '../auth/use-auth';
import { incidentsApi } from './api';
import { isAborted } from './messages';
import type { IncidentDetail } from './model';

type DetailState = {
  source: string;
  incident: IncidentDetail | null;
  error: unknown;
  loading: boolean;
};
const initial: DetailState = { source: '', incident: null, error: null, loading: true };

export function useIncidentDetail(id: string) {
  const { runProtected, pending } = useAuth();
  const [state, setState] = useState<DetailState>(initial);
  const [reload, setReload] = useState(0);
  const source = `${id}/${reload}`;
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      setState({ ...initial, source });
      if (pending) return;
      try {
        const incident = await runProtected(() =>
          incidentsApi.get(id, controller.signal),
        );
        if (!controller.signal.aborted)
          setState({ source, incident, error: null, loading: false });
      } catch (error) {
        if (!controller.signal.aborted && !isAborted(error))
          setState({ source, incident: null, error, loading: false });
      }
    }
    void load();
    return () => controller.abort();
  }, [id, source, pending, runProtected]);
  return {
    ...(state.source === source ? state : initial),
    retry: () => setReload((value) => value + 1),
  };
}

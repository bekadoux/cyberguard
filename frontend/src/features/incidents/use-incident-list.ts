import { useCallback, useEffect, useRef, useState } from 'react';
import { useAuth } from '../auth/use-auth';
import { incidentsApi } from './api';
import { isAborted } from './messages';
import { invalidResponse, type IncidentFilters, type IncidentSummary } from './model';

type ListState = {
  incidents: IncidentSummary[];
  cursor: string | null;
  phase: 'loading' | 'ready' | 'more' | 'error';
  error: unknown;
  source: string;
};
const initial: ListState = {
  incidents: [],
  cursor: null,
  phase: 'loading',
  error: null,
  source: '',
};

export function useIncidentList(filters: IncidentFilters | null) {
  const { runProtected, pending } = useAuth();
  const [state, setState] = useState<ListState>(initial);
  const [reload, setReload] = useState(0);
  const generation = useRef(0);
  const more = useRef<AbortController | null>(null);
  const status = filters?.status;
  const category = filters?.category;
  const source = `${status ?? 'invalid'}/${category ?? 'invalid'}/${reload}`;

  useEffect(() => {
    const version = ++generation.current;
    const controller = new AbortController();
    more.current?.abort();
    more.current = null;
    async function load() {
      setState({ ...initial, source });
      if (pending || status === undefined || category === undefined) return;
      try {
        const page = await runProtected(() =>
          incidentsApi.list({ status, category }, undefined, controller.signal),
        );
        if (generation.current === version && !controller.signal.aborted)
          setState({
            incidents: page.incidents,
            cursor: page.next_cursor,
            phase: 'ready',
            error: null,
            source,
          });
      } catch (error) {
        if (generation.current === version && !isAborted(error))
          setState({ ...initial, phase: 'error', error, source });
      }
    }
    void load();
    return () => {
      generation.current = version + 1;
      controller.abort();
      more.current?.abort();
      more.current = null;
    };
  }, [status, category, source, pending, runProtected]);

  const loadMore = useCallback(async () => {
    if (!state.cursor || more.current || status === undefined || category === undefined)
      return;
    const controller = new AbortController();
    more.current = controller;
    const version = generation.current;
    setState((previous) => ({ ...previous, phase: 'more', error: null }));
    try {
      const page = await runProtected(() =>
        incidentsApi.list({ status, category }, state.cursor!, controller.signal),
      );
      if (page.next_cursor === state.cursor) invalidResponse();
      if (version === generation.current && !controller.signal.aborted) {
        setState((previous) => ({
          incidents: [
            ...new Map(
              [...previous.incidents, ...page.incidents].map((incident) => [
                incident.id,
                incident,
              ]),
            ).values(),
          ],
          cursor: page.next_cursor,
          phase: 'ready',
          error: null,
          source,
        }));
      }
    } catch (error) {
      if (version === generation.current && !isAborted(error))
        setState((previous) => ({ ...previous, phase: 'ready', error }));
    } finally {
      if (more.current === controller) more.current = null;
    }
  }, [state.cursor, status, category, source, runProtected]);

  return {
    ...(state.source === source ? state : initial),
    loadMore,
    refresh: () => setReload((value) => value + 1),
  };
}

import { useSyncExternalStore } from 'react';
import { useSessionStore } from './auth-context';

export function useAuth() {
  const store = useSessionStore();
  const session = useSyncExternalStore(store.subscribe, store.getSnapshot);
  return { ...session, login: store.login, logout: store.logout, refresh: store.refresh };
}

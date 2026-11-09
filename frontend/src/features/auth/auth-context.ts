import { createContext, useContext } from 'react';
import type { SessionStore } from './session-store';

export const AuthContext = createContext<SessionStore | null>(null);

export function useSessionStore(): SessionStore {
  const store = useContext(AuthContext);
  if (!store) throw new Error('AuthProvider is required.');
  return store;
}

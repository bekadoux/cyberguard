import { useEffect, useState, type ReactNode } from 'react';
import { AuthContext } from './auth-context';
import { SessionStore } from './session-store';

export function AuthProvider({ children }: { children: ReactNode }) {
  const [store] = useState(() => new SessionStore());

  useEffect(() => {
    void store.refresh();
    const onVisible = () => {
      if (document.visibilityState === 'visible') void store.refresh();
    };
    window.addEventListener('focus', onVisible);
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      store.cancelRefresh();
      window.removeEventListener('focus', onVisible);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [store]);

  return <AuthContext.Provider value={store}>{children}</AuthContext.Provider>;
}

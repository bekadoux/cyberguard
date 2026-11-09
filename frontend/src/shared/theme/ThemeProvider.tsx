import { useEffect, useState, type ReactNode } from 'react';
import { ThemeContext, type ThemePreference } from './theme-context';

const key = 'cyberguard.theme';
const isPreference = (value: string | null): value is ThemePreference =>
  value === 'system' || value === 'light' || value === 'dark';

function readPreference(): ThemePreference {
  try {
    const stored = localStorage.getItem(key);
    return isPreference(stored) ? stored : 'system';
  } catch {
    return 'system';
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreference] = useState<ThemePreference>(readPreference);

  useEffect(() => {
    const media = matchMedia('(prefers-color-scheme: dark)');
    const apply = () => {
      const dark = preference === 'dark' || (preference === 'system' && media.matches);
      document.documentElement.dataset.theme = dark ? 'dark' : 'light';
      document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
      document
        .querySelector('meta[name="theme-color"]')
        ?.setAttribute('content', dark ? '#101010' : '#f5f7fa');
    };
    apply();
    media.addEventListener('change', apply);
    return () => media.removeEventListener('change', apply);
  }, [preference]);

  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === key || event.key === null) setPreference(readPreference());
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);

  const changePreference = (next: ThemePreference) => {
    setPreference(next);
    try {
      localStorage.setItem(key, next);
    } catch {
      // The current tab still supports switching themes when storage is unavailable.
    }
  };

  return (
    <ThemeContext.Provider value={{ preference, setPreference: changePreference }}>
      {children}
    </ThemeContext.Provider>
  );
}

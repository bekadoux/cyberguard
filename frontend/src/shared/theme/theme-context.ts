import { createContext, useContext } from 'react';

export type ThemePreference = 'system' | 'light' | 'dark';

export const ThemeContext = createContext<{
  preference: ThemePreference;
  setPreference: (preference: ThemePreference) => void;
} | null>(null);

export function useTheme() {
  const theme = useContext(ThemeContext);
  if (!theme) throw new Error('ThemeProvider is required.');
  return theme;
}

import { Monitor, Moon, Sun } from 'lucide-react';
import { useTheme, type ThemePreference } from '../theme/theme-context';

export function ThemeSelect() {
  const { preference, setPreference } = useTheme();
  const Icon = preference === 'system' ? Monitor : preference === 'dark' ? Moon : Sun;
  return (
    <label className="theme-select">
      <Icon size={16} aria-hidden="true" />
      <span className="sr-only">Color theme</span>
      <select
        value={preference}
        onChange={(event) => setPreference(event.target.value as ThemePreference)}
      >
        <option value="system">System</option>
        <option value="light">Light</option>
        <option value="dark">Dark</option>
      </select>
    </label>
  );
}

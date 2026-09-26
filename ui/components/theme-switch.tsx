'use client';

import { useSyncExternalStore } from 'react';
import { THEME_STORAGE_KEY } from '@/lib/client/theme';

type Theme = 'system' | 'light' | 'dark';

const CHANGE_EVENT = 'heliostat-theme-change';
const OPTIONS: { value: Theme; label: string }[] = [
  { value: 'system', label: 'Auto' },
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
];

function readTheme(): Theme {
  try {
    const value = localStorage.getItem(THEME_STORAGE_KEY);
    return value === 'light' || value === 'dark' ? value : 'system';
  } catch {
    return 'system';
  }
}

function subscribe(onChange: () => void) {
  window.addEventListener(CHANGE_EVENT, onChange);
  window.addEventListener('storage', onChange);
  return () => {
    window.removeEventListener(CHANGE_EVENT, onChange);
    window.removeEventListener('storage', onChange);
  };
}

function applyTheme(theme: Theme) {
  try {
    if (theme === 'system') localStorage.removeItem(THEME_STORAGE_KEY);
    else localStorage.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    // Storage can be unavailable (private mode); the choice still applies to this page.
  }
  if (theme === 'system') delete document.documentElement.dataset.theme;
  else document.documentElement.dataset.theme = theme;
  window.dispatchEvent(new Event(CHANGE_EVENT));
}

/** Auto (follow the OS) / Light / Dark. Stored per browser. */
export function ThemeSwitch() {
  const theme = useSyncExternalStore(subscribe, readTheme, () => 'system' as Theme);
  return (
    <div className="segmented theme-switch" role="group" aria-label="Color theme">
      {OPTIONS.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={theme === option.value}
          onClick={() => applyTheme(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

import { ConfigProvider as ModraConfigProvider, ThemeProvider as ModraThemeProvider } from '@modra/react';
import type { ThemeOverride } from '@modra/react';
import { zhCN } from '@modra/react/locale/zh-CN';
import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';

export type ThemePreference = 'system' | 'light' | 'dark';
export type EffectiveTheme = 'light' | 'dark';

interface AppearanceContextValue {
  preference: ThemePreference;
  effectiveTheme: EffectiveTheme;
  setPreference: (preference: ThemePreference) => void;
}

const STORAGE_KEY = 'yaginx-theme-preference';
const AppearanceContext = createContext<AppearanceContextValue | null>(null);

const getSystemTheme = (): EffectiveTheme =>
  window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';

const getStoredPreference = (): ThemePreference => {
  const stored = window.localStorage.getItem(STORAGE_KEY);
  return stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system';
};

/** yaginx 的品牌色与语义色，覆盖 Modra 默认主题。 */
export const buildModraTheme = (effectiveTheme: EffectiveTheme): ThemeOverride => {
  const isDark = effectiveTheme === 'dark';
  const primary = isDark ? '#76a8ff' : '#2575ff';
  return {
    color: {
      primary,
      primaryForeground: isDark ? '#0d1524' : '#ffffff',
      ring: primary,
      info: primary,
      success: isDark ? '#48d3bd' : '#00a88f',
      warning: isDark ? '#f0b661' : '#e99a2f',
      destructive: isDark ? '#ff918b' : '#d54b45',
    },
  };
};

export const AppearanceProvider: React.FC<React.PropsWithChildren> = ({ children }) => {
  const [preference, setPreferenceState] = useState<ThemePreference>(getStoredPreference);
  const [systemTheme, setSystemTheme] = useState<EffectiveTheme>(getSystemTheme);
  const effectiveTheme = preference === 'system' ? systemTheme : preference;

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    const onChange = () => setSystemTheme(media.matches ? 'dark' : 'light');
    media.addEventListener?.('change', onChange);
    return () => media.removeEventListener?.('change', onChange);
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = effectiveTheme;
    document.documentElement.style.colorScheme = effectiveTheme;
    document.documentElement.classList.toggle('dark', effectiveTheme === 'dark');
  }, [effectiveTheme]);

  const setPreference = (nextPreference: ThemePreference) => {
    window.localStorage.setItem(STORAGE_KEY, nextPreference);
    setPreferenceState(nextPreference);
  };

  const value = useMemo(() => ({ preference, effectiveTheme, setPreference }), [preference, effectiveTheme]);
  const modraTheme = useMemo(() => buildModraTheme(effectiveTheme), [effectiveTheme]);

  return (
    <AppearanceContext.Provider value={value}>
      <ModraThemeProvider theme={modraTheme} applyToRoot>
        <ModraConfigProvider locale={zhCN} size="admin">
          {children}
        </ModraConfigProvider>
      </ModraThemeProvider>
    </AppearanceContext.Provider>
  );
};

export const useAppearance = (): AppearanceContextValue => {
  const context = useContext(AppearanceContext);
  if (!context) {
    throw new Error('useAppearance must be used inside AppearanceProvider');
  }
  return context;
};

export const chartPalette = (effectiveTheme: EffectiveTheme) => effectiveTheme === 'dark'
  ? { flow: '#76a8ff', signal: '#48d3bd', warning: '#f0b661', accent: '#b69cff', alert: '#ff918b', neutral: '#9baac0', grid: '#2a3a53', text: '#d3ddec' }
  : { flow: '#2575ff', signal: '#00a88f', warning: '#e99a2f', accent: '#7357d6', alert: '#d54b45', neutral: '#66748a', grid: '#dbe4ef', text: '#526176' };

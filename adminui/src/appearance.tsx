import { ConfigProvider, theme } from 'antd';
import type { ThemeConfig } from 'antd';
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

export const buildTheme = (effectiveTheme: EffectiveTheme): ThemeConfig => {
  const isDark = effectiveTheme === 'dark';

  return {
    algorithm: isDark ? theme.darkAlgorithm : theme.defaultAlgorithm,
    token: {
      colorPrimary: isDark ? '#76a8ff' : '#2575ff',
      colorInfo: isDark ? '#76a8ff' : '#2575ff',
      colorSuccess: isDark ? '#48d3bd' : '#00a88f',
      colorWarning: isDark ? '#f0b661' : '#e99a2f',
      colorError: isDark ? '#ff918b' : '#d54b45',
      colorBgBase: isDark ? '#0d1524' : '#f4f7fb',
      colorTextBase: isDark ? '#edf4ff' : '#172033',
      colorBorder: isDark ? '#2a3a53' : '#dbe4ef',
      borderRadius: 10,
      borderRadiusLG: 14,
      fontFamily: '"Noto Sans SC", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
      fontSize: 14,
      wireframe: false,
      controlHeight: 30,
      controlHeightSM: 22,
      controlHeightLG: 36,
    },
    components: {
      Layout: {
        bodyBg: isDark ? '#0d1524' : '#f4f7fb',
        headerBg: isDark ? '#121e31' : '#ffffff',
        siderBg: isDark ? '#0b1220' : '#ffffff',
        triggerBg: isDark ? '#0b1220' : '#ffffff',
      },
      Menu: {
        itemBg: isDark ? '#0b1220' : '#ffffff',
        subMenuItemBg: isDark ? '#0b1220' : '#ffffff',
        itemColor: isDark ? '#edf4ff' : '#172033',
        itemHoverColor: isDark ? '#76a8ff' : '#2575ff',
        itemHoverBg: isDark ? '#142842' : '#edf3ff',
        itemSelectedColor: isDark ? '#76a8ff' : '#2575ff',
        itemSelectedBg: isDark ? '#173b68' : '#e6efff',
        darkItemBg: '#0b1220',
        darkSubMenuItemBg: '#0b1220',
        darkItemColor: '#edf4ff',
        darkItemHoverColor: '#76a8ff',
        darkItemHoverBg: '#142842',
        darkItemSelectedColor: '#76a8ff',
        darkItemSelectedBg: '#173b68',
        itemBorderRadius: 8,
      },
      Card: {
        headerBg: 'transparent',
      },
      Table: {
        headerBg: isDark ? '#18243a' : '#f7f9fc',
        headerColor: isDark ? '#dbe7fa' : '#526176',
        cellPaddingBlockSM: 6,
        cellPaddingInlineSM: 8,
      },
    },
  };
};

/** 与 buildTheme 使用同一组品牌色，让迁移期间 antd 与 Modra 组件外观一致。 */
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
  const antTheme = useMemo(() => buildTheme(effectiveTheme), [effectiveTheme]);
  const modraTheme = useMemo(() => buildModraTheme(effectiveTheme), [effectiveTheme]);

  return (
    <AppearanceContext.Provider value={value}>
      <ConfigProvider theme={antTheme}>
        <ModraThemeProvider theme={modraTheme} applyToRoot>
          <ModraConfigProvider locale={zhCN}>
            {children}
          </ModraConfigProvider>
        </ModraThemeProvider>
      </ConfigProvider>
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

import { describe, expect, it } from 'vitest';
import {
  persistSidebarCollapsed,
  readSidebarCollapsed,
  SIDEBAR_COLLAPSED_STORAGE_KEY,
} from './sidebarPreference';

const testStorage = (initial: Record<string, string> = {}) => {
  const values = new Map(Object.entries(initial));
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
  };
};

describe('sidebar preference', () => {
  it('defaults to expanded when the value is missing or invalid', () => {
    expect(readSidebarCollapsed(testStorage())).toBe(false);
    expect(readSidebarCollapsed(testStorage({ [SIDEBAR_COLLAPSED_STORAGE_KEY]: 'collapsed' }))).toBe(false);
    expect(readSidebarCollapsed(testStorage({ [SIDEBAR_COLLAPSED_STORAGE_KEY]: 'false' }))).toBe(false);
  });

  it('persists collapse and expansion through the same storage key', () => {
    const storage = testStorage();

    persistSidebarCollapsed(true, storage);
    expect(readSidebarCollapsed(storage)).toBe(true);

    persistSidebarCollapsed(false, storage);
    expect(readSidebarCollapsed(storage)).toBe(false);
  });
});

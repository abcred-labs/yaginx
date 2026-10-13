export const SIDEBAR_COLLAPSED_STORAGE_KEY = 'yaginx.sidebar.collapsed';

type SidebarStorage = Pick<Storage, 'getItem' | 'setItem'>;

const browserStorage = (): SidebarStorage | null => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};

export const readSidebarCollapsed = (storage: SidebarStorage | null = browserStorage()): boolean => {
  if (!storage) {
    return false;
  }

  try {
    return JSON.parse(storage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY) ?? 'false') === true;
  } catch {
    return false;
  }
};

export const persistSidebarCollapsed = (collapsed: boolean, storage: SidebarStorage | null = browserStorage()) => {
  try {
    storage?.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, JSON.stringify(collapsed));
  } catch {
    // Storage can be unavailable in private browsing or restricted contexts.
  }
};

export interface AccountDisplay {
  email: string;
  localName: string;
  domainName: string;
  initial: string;
}

export const getAccountDisplay = (email?: string | null): AccountDisplay => {
  const normalizedEmail = email?.trim() ?? "";
  const separatorIndex = normalizedEmail.indexOf("@");

  if (separatorIndex < 0) {
    const localName = normalizedEmail || "?";
    return {
      email: normalizedEmail || "Unknown account",
      localName,
      domainName: "",
      initial: localName.charAt(0).toUpperCase() || "?",
    };
  }

  const localName = normalizedEmail.slice(0, separatorIndex) || "?";
  const domainName = normalizedEmail.slice(separatorIndex + 1);

  return {
    email: normalizedEmail || "Unknown account",
    localName,
    domainName,
    initial: localName.charAt(0).toUpperCase() || "?",
  };
};

export const getSessionRemainingSeconds = (tokenExpireTime: number, now = Date.now()): number => {
  if (!Number.isFinite(tokenExpireTime) || tokenExpireTime <= 0) {
    return 0;
  }

  return Math.max(0, Math.ceil(tokenExpireTime - now / 1000));
};

export const formatSessionRemaining = (seconds: number): string => {
  const safeSeconds = Math.max(0, Math.ceil(Number.isFinite(seconds) ? seconds : 0));
  const minutes = Math.floor(safeSeconds / 60);
  const remainingSeconds = safeSeconds % 60;

  return minutes > 0 ? `${minutes}m ${remainingSeconds}s` : `${remainingSeconds}s`;
};

export const isFirstPartyApiUrl = (url?: string): boolean => {
  if (!url || url.includes("/webapi/central/")) {
    return false;
  }

  try {
    const pathname = new URL(url, "http://yaginx.local").pathname;
    return pathname === "/api" || pathname.includes("/api/");
  } catch {
    return false;
  }
};

// Management APIs are a lowerCamelCase v2 contract. Do not silently repair
// PascalCase or field aliases here: unexpected payloads must fail visibly.
export const normalizeApiResponse = <T>(value: T): T => value;

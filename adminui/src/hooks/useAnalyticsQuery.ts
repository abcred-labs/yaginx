import { useCallback, useEffect, useState } from "react";

export const useAnalyticsQuery = <T>(load: (signal: AbortSignal) => Promise<T>, deps: readonly unknown[]) => {
  const [data, setData] = useState<T>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>();
  const [refreshToken, setRefreshToken] = useState(0);
  const refresh = useCallback(() => setRefreshToken((value) => value + 1), []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(undefined);
    load(controller.signal).then((value) => { if (!controller.signal.aborted) setData(value); })
      .catch((reason) => { if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "请求失败"); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
    // refreshToken is intentionally part of the request identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, refreshToken]);
  return { data, setData, loading, error, refresh };
};

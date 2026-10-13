import { FETCH_RETRY_DELAY, FETCH_TIMEOUT } from "./requestConsts";

export async function FetchInternal(
  input: RequestInfo,
  init?: RequestInit,
  retryCount: number = 0
): Promise<unknown> {
  let remainingRetries = retryCount;
  const controller = new AbortController();
  const timeout = FETCH_TIMEOUT ? setTimeout(() => controller.abort(), FETCH_TIMEOUT) : undefined;
  const requestInit = { ...init, signal: init?.signal ?? controller.signal };
  try {
    for (;;) {
      try {
        const response = await fetch(input, requestInit);
        // First-party APIs use a JSON envelope for both success and failure.
        // Preserve it so FetchJson can show a safe message and trace ID.
        return await response.json();
      } catch (error) {
        if (remainingRetries <= 0) {
          if (controller.signal.aborted) {
            throw new Error("request timeout");
          }
          throw error;
        }
        remainingRetries--;
        await new Promise((resolve) => setTimeout(resolve, FETCH_RETRY_DELAY));
      }
    }
  } finally {
    if (timeout) {
      clearTimeout(timeout);
    }
  }
}

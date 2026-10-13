import { FetchInternal } from "./FetchInternal";
import { getHeaders } from "./getHeaders";


export async function Fetch(
  input: RequestInfo,
  init?: RequestInit,
  retryCount: number = 0,
  authHeader?: Record<string, string>
) {

  const authorizationHeaders = authHeader ?? getHeaders();
  const updatedHeaders = new Headers(init?.headers);
  for (const key in authorizationHeaders) {
    const value = authorizationHeaders[key];
    updatedHeaders.append(key, value);
  }
  // for (const [name, value] of additionalHeaders.entries()) {
  //   updatedHeaders.append(name, value);
  // }

  const additionalHeaders: Record<string, string> = {
    // "Content-Type": "application/json",
    "x-requested-with": "XMLHttpRequest",
  };
  for (const key in additionalHeaders) {
    const value = additionalHeaders[key];
    updatedHeaders.append(key, value);
  }

  const updatedRequestOptions = {
    ...init,
    headers: updatedHeaders
  };

  return FetchInternal(input, updatedRequestOptions, retryCount);
}

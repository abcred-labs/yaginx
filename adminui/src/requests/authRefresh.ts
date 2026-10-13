import { FetchInternal } from "./FetchInternal";
import { PREFIX } from "./requestConsts";
import { API_BASE_URL } from "../../config/constant";

export interface RefreshSessionResponse {
  code: number;
  data?: { token: string; expiresIn: number; name: string; userId: string };
}

// Keep refresh independent from api/account and FetchJson to avoid a request
// module cycle through the auth store.
export const refreshSession = (): Promise<RefreshSessionResponse> =>
  FetchInternal(`${PREFIX}${API_BASE_URL}/api/account/refresh_token`, {
    method: "GET",
    headers: { "x-requested-with": "XMLHttpRequest" },
  }) as Promise<RefreshSessionResponse>;

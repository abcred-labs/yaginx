import { FetchJson } from "@/requests/FetchJson";

export type LoginRequest = { email: string; password: string; remember?: boolean };
export type LoginResponse = { token: string; expiresIn: number; name: string; userId: string };
export type Profile = { email: string };
export type PasswordRecoveryStartResponse = { recoveryId: string; maskedEmail: string };
export type ProfileUpdateRequest = {
  email?: string;
  oldPassword?: string;
  newPassword?: string;
  confirmPassword?: string;
};
export type SessionSummary = {
  id: string;
  remembered: boolean;
  userAgent: string;
  clientIp: string;
  issuedAt: string;
  lastUsedAt: string;
  idleExpiresAt: string;
  absoluteExpiresAt: string;
  current: boolean;
};

export const login = (data: LoginRequest) => FetchJson<LoginResponse>({ url: '/api/account/login', method: 'POST', data });
export const getProfile = () => FetchJson<Profile>({ url: '/api/account/profile', method: 'GET' });
export const updateProfile = (data: ProfileUpdateRequest) => FetchJson<boolean>({ url: '/api/account/update_profile', method: 'POST', data });

// refreshToken authenticates via the HttpOnly yaginx-refresh cookie, not a
// bearer token, so it keeps working after the access token itself has
// expired. ignoreGlobalError: true because a failed refresh is routine (no
// prior session, or one that expired) and must not trip FetchJson's 401
// force-logout — accessStore decides what a failure means here.
export const refreshToken = () => FetchJson<LoginResponse>({ url: '/api/account/refresh_token', method: 'GET', ignoreGlobalError: true });

export const logout = () => FetchJson<boolean>({ url: '/api/account/logout', method: 'POST', ignoreGlobalError: true });

export const listSessions = () => FetchJson<SessionSummary[]>({ url: '/api/account/sessions', method: 'GET' });

export const revokeSession = (id: string) => FetchJson<boolean>({ url: `/api/account/sessions/${id}/revoke`, method: 'POST' });

export const revokeOtherSessions = () => FetchJson<boolean>({ url: '/api/account/sessions/revoke_others', method: 'POST' });

export const startPasswordRecovery = (data: { accountEmail: string }) =>
  FetchJson<PasswordRecoveryStartResponse>({ url: '/api/account/password_recovery/start', method: 'POST', data });

export const verifyPasswordRecoveryEmail = (data: { recoveryId: string; email: string }) =>
  FetchJson<boolean>({ url: '/api/account/password_recovery/verify_email', method: 'POST', data });

export const resetPasswordRecovery = (data: {
  recoveryId: string;
  code: string;
  newPassword: string;
  confirmPassword: string;
}) => FetchJson<boolean>({ url: '/api/account/password_recovery/reset', method: 'POST', data });

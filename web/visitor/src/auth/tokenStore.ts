// Module-level холдер access-токена. Access живёт ТОЛЬКО в памяти (не в
// localStorage — защита от XSS). Refresh хранится в HttpOnly-cookie (браузер
// шлёт его сам на /api/v1/auth/*), фронт его не видит.

let accessToken: string | null = null;
let onUnauthorized: (() => void) | null = null;

export function getAccessToken(): string | null {
  return accessToken;
}

export function setAccessToken(token: string | null): void {
  accessToken = token;
}

export function clearAccessToken(): void {
  accessToken = null;
}

/** Колбэк, вызываемый когда сессия окончательно протухла (refresh не удался). */
export function setOnUnauthorized(cb: (() => void) | null): void {
  onUnauthorized = cb;
}

export function notifyUnauthorized(): void {
  onUnauthorized?.();
}

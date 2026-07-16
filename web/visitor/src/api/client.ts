import type {
  AcceptResponse,
  ApiErrorEnvelope,
  AuthUser,
  CatalogResponse,
  CreateGrantResponse,
  CreateOwnerResponse,
  DevicesResponse,
  InitiateResponse,
  LoginResponse,
  OpenDoorResponse,
  OtpSendResponse,
  QrParams,
  RefreshResponse,
  ResidentsResponse,
  ValidateResponse,
} from './types';
import {
  clearAccessToken,
  getAccessToken,
  notifyUnauthorized,
  setAccessToken,
} from '../auth/tokenStore';

/**
 * Базовый origin backend. Пусто по умолчанию — используем относительный /api,
 * который Vite-proxy направляет на :8080 (снимает CORS в деве). Same-origin →
 * refresh-cookie (HttpOnly, Path=/api/v1/auth) браузер шлёт сам.
 */
export const API_ORIGIN = import.meta.env.VITE_API_URL ?? '';
const API_BASE = `${API_ORIGIN}/api/v1`;

/** Абсолютный/относительный путь к API (для fetch и EventSource). */
export function apiUrl(path: string): string {
  return `${API_ORIGIN}${path}`;
}

/** Типизированная ошибка API — несёт код и request_id из конверта. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId: string;

  constructor(status: number, code: string, message: string, requestId: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

interface RequestOpts {
  /** Не добавлять Bearer и не пытаться refresh (для auth-эндпоинтов). */
  skipAuth?: boolean;
  /** Внутренний флаг: запрос уже повторён после refresh. */
  retried?: boolean;
}

// --- Single-flight refresh: один общий промис на параллельные 401 ---
let refreshPromise: Promise<boolean> | null = null;

async function refreshAccess(): Promise<boolean> {
  if (!refreshPromise) {
    refreshPromise = doRefresh().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
}

async function doRefresh(): Promise<boolean> {
  try {
    const res = await fetch(`${API_BASE}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
    });
    if (!res.ok) return false;
    const body = (await res.json()) as RefreshResponse;
    if (body?.access_token) {
      setAccessToken(body.access_token);
      return true;
    }
    return false;
  } catch {
    return false;
  }
}

async function request<T>(path: string, init?: RequestInit, opts: RequestOpts = {}): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...((init?.headers as Record<string, string>) ?? {}),
  };
  const token = getAccessToken();
  if (token && !opts.skipAuth) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, { ...init, headers });
  } catch {
    throw new ApiError(0, 'NETWORK', 'Нет связи с сервером', '');
  }

  // 401 → попытка refresh (single-flight) + один ретрай. Не для auth-эндпоинтов
  // (skipAuth) и не для уже повторённых запросов (без зацикливания).
  if (res.status === 401 && !opts.skipAuth && !opts.retried) {
    const ok = await refreshAccess();
    if (ok) {
      return request<T>(path, init, { ...opts, retried: true });
    }
    clearAccessToken();
    notifyUnauthorized();
    throw new ApiError(401, 'UNAUTHORIZED', 'Сессия истекла — войдите снова', '');
  }

  if (res.status === 204) {
    return undefined as T;
  }

  const text = await res.text();
  let body: unknown = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = null;
    }
  }

  if (!res.ok) {
    const envelope = body as Partial<ApiErrorEnvelope> | null;
    const err = envelope?.error;
    throw new ApiError(
      res.status,
      err?.code ?? 'INTERNAL',
      err?.message ?? res.statusText ?? 'Ошибка сервера',
      err?.request_id ?? '',
    );
  }

  return body as T;
}

export const api = {
  // --- Публичные (визитёр) ---
  validateQr: (p: QrParams) =>
    request<ValidateResponse>('/qr/validate', { method: 'POST', body: JSON.stringify(p) }),

  initiateCall: (p: QrParams) =>
    request<InitiateResponse>('/calls/initiate', { method: 'POST', body: JSON.stringify(p) }),

  cancelCall: (callId: string) =>
    request<void>(`/calls/${encodeURIComponent(callId)}/cancel`, { method: 'POST' }),

  endCall: (callId: string) =>
    request<void>(`/calls/${encodeURIComponent(callId)}/end`, { method: 'POST' }),

  // --- Защищённые (resident/owner, Bearer добавляется автоматически) ---
  acceptCall: (callId: string) =>
    request<AcceptResponse>(`/calls/${encodeURIComponent(callId)}/accept`, { method: 'POST' }),

  openDoor: (callId: string) =>
    request<OpenDoorResponse>('/access/open', {
      method: 'POST',
      body: JSON.stringify({ call_id: callId }),
    }),

  listDevices: () => request<DevicesResponse>('/devices'),

  // --- Auth ---
  authOtpSend: (phone: string) =>
    request<OtpSendResponse>('/auth/otp/send', { method: 'POST', body: JSON.stringify({ phone }) }, { skipAuth: true }),

  authOtpVerify: (phone: string, code: string) =>
    request<LoginResponse>('/auth/otp/verify', { method: 'POST', body: JSON.stringify({ phone, code }) }, { skipAuth: true }),

  authAdminLogin: (email: string, password: string, totp_code: string) =>
    request<LoginResponse>('/auth/admin/login', { method: 'POST', body: JSON.stringify({ email, password, totp_code }) }, { skipAuth: true }),

  authRefresh: () =>
    request<RefreshResponse>('/auth/refresh', { method: 'POST' }, { skipAuth: true }),

  authLogout: () =>
    request<void>('/auth/logout', { method: 'POST' }, { skipAuth: true }),

  authMe: () => request<AuthUser>('/auth/me'),

  // --- Онбординг: УК-консоль (admin, Bearer добавляется автоматически) ---
  adminCatalog: () => request<CatalogResponse>('/admin/catalog'),

  adminCreateOwner: (
    apartment_id: string,
    phone: string,
    full_name: string,
    access_point_public_ids: string[],
  ) =>
    request<CreateOwnerResponse>('/admin/owners', {
      method: 'POST',
      body: JSON.stringify({ apartment_id, phone, full_name, access_point_public_ids }),
    }),

  adminCreateGrant: (access_point_public_id: string, phone: string, full_name: string) =>
    request<CreateGrantResponse>('/admin/access-grants', {
      method: 'POST',
      body: JSON.stringify({ access_point_public_id, phone, full_name }),
    }),

  adminListResidents: () => request<ResidentsResponse>('/admin/residents'),
};

/** Человекочитаемое сообщение из любой ошибки. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message || e.code;
  if (e instanceof Error) return e.message;
  return 'Неизвестная ошибка';
}

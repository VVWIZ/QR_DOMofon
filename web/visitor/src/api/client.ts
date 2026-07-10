import type {
  AcceptResponse,
  ApiErrorEnvelope,
  DevicesResponse,
  InitiateResponse,
  OpenDoorResponse,
  QrParams,
  ValidateResponse,
} from './types';

/**
 * Базовый origin backend. Пусто по умолчанию — используем относительный /api,
 * который Vite-proxy направляет на :8080 (снимает CORS в деве).
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

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${API_BASE}${path}`, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        ...(init?.headers ?? {}),
      },
    });
  } catch {
    throw new ApiError(0, 'NETWORK', 'Нет связи с сервером', '');
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
  validateQr: (p: QrParams) =>
    request<ValidateResponse>('/qr/validate', { method: 'POST', body: JSON.stringify(p) }),

  initiateCall: (p: QrParams) =>
    request<InitiateResponse>('/calls/initiate', { method: 'POST', body: JSON.stringify(p) }),

  acceptCall: (callId: string) =>
    request<AcceptResponse>(`/calls/${encodeURIComponent(callId)}/accept`, { method: 'POST' }),

  cancelCall: (callId: string) =>
    request<void>(`/calls/${encodeURIComponent(callId)}/cancel`, { method: 'POST' }),

  endCall: (callId: string) =>
    request<void>(`/calls/${encodeURIComponent(callId)}/end`, { method: 'POST' }),

  openDoor: (callId: string) =>
    request<OpenDoorResponse>('/access/open', {
      method: 'POST',
      body: JSON.stringify({ call_id: callId }),
    }),

  listDevices: () => request<DevicesResponse>('/devices'),
};

/** Человекочитаемое сообщение из любой ошибки. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message || e.code;
  if (e instanceof Error) return e.message;
  return 'Неизвестная ошибка';
}

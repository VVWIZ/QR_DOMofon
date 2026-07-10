// Типы контрактов из docs/skeleton/api.md (источник истины).

/** Единый конверт ошибок backend (ТЗ §13.1). */
export interface ApiErrorEnvelope {
  error: {
    code: string;
    message: string;
    request_id: string;
  };
}

/** Известные коды ошибок (см. таблицу в api.md). */
export type ApiErrorCode =
  | 'INVALID_QR'
  | 'VALIDATION_ERROR'
  | 'CALL_NOT_FOUND'
  | 'CALL_IN_PROGRESS'
  | 'DEVICE_OFFLINE'
  | 'INTERNAL';

/** Параметры QR из ссылки посетителя: /v?aid&v&kid&sig. */
export interface QrParams {
  aid: string;
  v: string;
  kid: string;
  sig: string;
}

export type DeviceStatus = 'online' | 'offline';

export interface AccessPoint {
  public_id: string;
  label: string;
}

export interface Apartment {
  id: string;
  number: string;
}

/** Ответ POST /api/v1/qr/validate (200). */
export interface ValidateResponse {
  access_point: AccessPoint;
  apartment: Apartment;
  device_status: DeviceStatus;
  /** Текст предупреждения при offline-устройстве (звонок не блокируется). */
  warning?: string;
}

/** Ответ POST /api/v1/calls/initiate (200). */
export interface InitiateResponse {
  call_id: string;
  room: string;
  livekit_url: string;
  visitor_token: string;
  device_status: DeviceStatus;
}

/** Ответ POST /api/v1/calls/{id}/accept (200). */
export interface AcceptResponse {
  room: string;
  livekit_url: string;
  resident_token: string;
}

/** Ответ POST /api/v1/access/open (200). */
export interface OpenDoorResponse {
  request_id: string;
  status: string;
}

export interface Device {
  id: string;
  serial: string;
  access_point_id?: string;
  type?: string;
  firmware_version?: string;
  status: DeviceStatus;
  last_seen_at: string;
}

/** Ответ GET /api/v1/devices (200) — обёрнут в объект. */
export interface DevicesResponse {
  devices: Device[];
}

// --- SSE-события GET /api/v1/resident/events ---

export interface SseCallIncoming {
  call_id: string;
  access_point_label: string;
  apartment_id: string;
}

export interface SseCallCancelled {
  call_id: string;
}

export interface SseCallAccepted {
  call_id: string;
}

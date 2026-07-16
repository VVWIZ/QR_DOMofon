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
  | 'CALL_NOT_ACCEPTED'
  | 'CALL_IN_PROGRESS'
  | 'DEVICE_OFFLINE'
  | 'UNAUTHORIZED'
  | 'FORBIDDEN'
  | 'RATE_LIMIT'
  | 'INVITE_INVALID'
  | 'INVITE_EXPIRED'
  | 'INTERNAL';

// --- Auth (auth.md, api.md «Аутентификация») ---

export type UserKind = 'resident' | 'owner' | 'mc_admin' | 'system_admin';

export interface AuthApartment {
  id: string;
  role: string;
}

/** Профиль пользователя (ответ /auth/me и поле user в логине). */
export interface AuthUser {
  id: string;
  kind: UserKind;
  apartments: AuthApartment[];
  mc_id: string | null;
}

/** Ответ otp/verify и admin/login. */
export interface LoginResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
  user: AuthUser;
}

/** Ответ /auth/refresh (без user). */
export interface RefreshResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
}

/** Ответ /auth/otp/send (dev_code только в dev-режиме). */
export interface OtpSendResponse {
  sent: boolean;
  dev_code?: string;
}

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
  last_seen_at: string | null;
}

/** Ответ GET /api/v1/devices (200) — обёрнут в объект. */
export interface DevicesResponse {
  devices: Device[];
}

// --- Онбординг: УК-консоль (api.md, эндпоинты /admin/*) ---

/** Выданный инвайт (мок доставки: секрет-ссылка в ответе). */
export interface InviteInfo {
  token: string;
  url: string;
  expires_at: string;
}

/** Ответ POST /api/v1/admin/owners (201). */
export interface CreateOwnerResponse {
  invite: InviteInfo;
}

/**
 * Ответ POST /api/v1/admin/access-grants: грант выдан сразу (200,
 * granted=true) либо выпущен инвайт для активации (201, granted=false).
 */
export interface CreateGrantResponse {
  granted: boolean;
  user_id?: string;
  access_point_public_id?: string;
  invite?: InviteInfo;
}

export interface ResidentApartmentInfo {
  id: string;
  number: string;
  role: string;
}

export interface ResidentGrantInfo {
  public_id: string;
  label: string;
}

/** Жилец/владелец в выборке УК (GET /api/v1/admin/residents). */
export interface ResidentInfo {
  user_id: string;
  phone: string;
  full_name: string;
  kind: UserKind;
  apartments: ResidentApartmentInfo[];
  grants: ResidentGrantInfo[];
}

/** Ответ GET /api/v1/admin/residents (200). */
export interface ResidentsResponse {
  residents: ResidentInfo[];
}

// --- Матрица доступа УК (инкремент D) ---

export interface AdminSite {
  id: string;
  name: string;
  address: string;
  kind: 'complex' | 'standalone';
}
export interface AdminSitesResponse {
  sites: AdminSite[];
}
export interface MatrixPoint {
  public_id: string;
  label: string;
  type: 'gate' | 'barrier';
}
export interface MatrixApartment {
  id: string;
  number: string;
}
export interface MatrixOwner {
  user_id: string;
  full_name: string;
  phone: string;
  apartments: MatrixApartment[];
  grants: string[]; // public_id точек с грантом
}
export interface MatrixResponse {
  points: MatrixPoint[];
  owners: MatrixOwner[];
}
export interface MatrixResident {
  user_id: string;
  full_name: string;
  phone: string;
  grants: string[];
}
export interface ApartmentResidentsResponse {
  residents: MatrixResident[];
}

// --- Каталог УК (GET /api/v1/admin/catalog) для выпадашек формы ---

export interface CatalogApartment {
  id: string;
  number: string;
}

export interface CatalogEntrance {
  id: string; // "" → квартиры без подъезда
  number: string;
  apartments: CatalogApartment[];
}

export interface CatalogBuilding {
  id: string;
  address: string;
  entrances: CatalogEntrance[];
}

export interface CatalogPoint {
  public_id: string;
  label: string;
  type: 'gate' | 'barrier';
}

export interface CatalogResponse {
  buildings: CatalogBuilding[];
  points: CatalogPoint[];
}

// --- Платформенная админка (/system, роль system_admin) ---

export interface SysMC {
  id: string;
  name: string;
  sites: number;
  buildings: number;
}
export interface SysMCsResponse {
  management_companies: SysMC[];
}
export interface SysCreateIdResponse {
  id: string;
}
export interface SysCreateAdminResponse {
  user_id: string;
  otpauth_url: string;
}
export interface SysEntrance {
  id: string;
  number: string;
}
export interface SysBuilding {
  id: string;
  address: string;
  entrances: SysEntrance[];
}
export interface SysSite {
  id: string;
  name: string;
  address: string;
  kind: 'complex' | 'standalone';
  buildings: SysBuilding[];
}
export interface SysCatalogResponse {
  sites: SysSite[];
}

// --- Гостевой доступ (публичная страница /g/{token}) ---

export interface GuestPoint {
  public_id: string;
  label: string;
  type: 'entrance' | 'gate' | 'barrier';
  online: boolean;
}

/** Ответ GET /api/v1/g/{token} (200). */
export interface GuestViewResponse {
  guest_name: string;
  valid_from: string;
  valid_to: string;
  points: GuestPoint[];
}

/** Ответ POST /api/v1/g/{token}/open (200). */
export interface GuestOpenResponse {
  request_id: string;
  status: string;
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

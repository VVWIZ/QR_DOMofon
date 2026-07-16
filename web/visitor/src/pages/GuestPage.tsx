import { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { api, ApiError, errorMessage } from '../api/client';
import type { GuestPoint, GuestViewResponse } from '../api/types';

type Toast = { kind: 'ok' | 'err'; text: string } | null;

/** Заголовок точки по типу. */
function pointKindLabel(type: GuestPoint['type']): string {
  if (type === 'gate') return 'Калитка';
  if (type === 'barrier') return 'Шлагбаум';
  return 'Подъезд';
}

function loadErr(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'GUEST_INVALID') return 'Ссылка недействительна.';
    if (e.code === 'GUEST_EXPIRED') return 'Срок действия ссылки истёк или доступ отозван.';
  }
  return errorMessage(e);
}

function openErr(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'DEVICE_OFFLINE') return 'Устройство недоступно — попробуйте позже.';
    if (e.code === 'FORBIDDEN') return 'Эта точка сейчас недоступна.';
    if (e.code === 'GUEST_EXPIRED') return 'Срок доступа истёк.';
    if (e.code === 'GUEST_INVALID') return 'Ссылка недействительна.';
    if (e.code === 'RATE_LIMIT') return 'Слишком часто — подождите немного.';
  }
  return errorMessage(e);
}

export function GuestPage() {
  const { token = '' } = useParams();
  const [view, setView] = useState<GuestViewResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [opening, setOpening] = useState<string | null>(null); // public_id в процессе
  const [toast, setToast] = useState<Toast>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadError(null);
    try {
      setView(await api.guestView(token));
    } catch (e) {
      setLoadError(loadErr(e));
    } finally {
      setLoading(false);
    }
  }, [token]);

  useEffect(() => {
    void load();
  }, [load]);

  const open = async (p: GuestPoint) => {
    setOpening(p.public_id);
    setToast(null);
    try {
      await api.guestOpen(token, p.public_id);
      setToast({ kind: 'ok', text: `${pointKindLabel(p.type)} «${p.label}» — команда отправлена` });
    } catch (e) {
      setToast({ kind: 'err', text: openErr(e) });
    } finally {
      setOpening(null);
      setTimeout(() => setToast(null), 3000);
    }
  };

  if (loading) {
    return (
      <div className="page center">
        <div className="spinner" />
      </div>
    );
  }

  if (loadError) {
    return (
      <div className="page center">
        <div className="card center">
          <h1>Гостевой доступ</h1>
          <p className="err-text">{loadError}</p>
        </div>
      </div>
    );
  }

  if (!view) return null;

  const validTo = new Date(view.valid_to);

  return (
    <div className="page guest">
      <div className="card">
        <h1>Здравствуйте, {view.guest_name}</h1>
        <p className="muted">
          Гостевой доступ действует до <b>{validTo.toLocaleString('ru-RU')}</b>
        </p>

        <div className="guest-points">
          {view.points.length === 0 && <p className="muted">Нет доступных точек.</p>}
          {view.points.map((p) => (
            <button
              key={p.public_id}
              className="btn success big guest-open-btn"
              onClick={() => open(p)}
              disabled={opening !== null || !p.online}
            >
              <span>
                {pointKindLabel(p.type)}: {p.label}
              </span>
              <span className="status-row">
                <span className={`dot ${p.online ? 'on' : 'off'}`} />
                {opening === p.public_id ? 'Открываю…' : p.online ? 'Открыть' : 'Не в сети'}
              </span>
            </button>
          ))}
        </div>

        <p className="hint" style={{ marginTop: 16 }}>
          Кнопка открывает дверь/ворота удалённо. Доступ ограничен по времени и может быть отозван.
        </p>
      </div>

      {toast && <div className={`toast ${toast.kind}`}>{toast.text}</div>}
    </div>
  );
}

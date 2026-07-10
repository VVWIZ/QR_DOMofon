import { useCallback, useEffect, useState } from 'react';
import { api, apiUrl, ApiError, errorMessage } from '../api/client';
import type {
  AcceptResponse,
  Device,
  SseCallAccepted,
  SseCallCancelled,
  SseCallIncoming,
} from '../api/types';
import { CallRoom } from '../components/CallRoom';
import { useSSE, type SSEStatus } from '../hooks/useSSE';

interface ActiveCall extends AcceptResponse {
  call_id: string;
  access_point_label: string;
}

interface Toast {
  kind: 'ok' | 'err';
  text: string;
}

const SSE_PATH = apiUrl('/api/v1/resident/events');

export function ResidentPage() {
  const [incoming, setIncoming] = useState<SseCallIncoming | null>(null);
  const [active, setActive] = useState<ActiveCall | null>(null);
  const [sseStatus, setSseStatus] = useState<SSEStatus>('connecting');
  const [devices, setDevices] = useState<Device[] | null>(null);
  const [toast, setToast] = useState<Toast | null>(null);
  const [accepting, setAccepting] = useState(false);

  // --- SSE-поток событий жильца ---
  const onIncoming = useCallback((data: unknown) => {
    setIncoming(data as SseCallIncoming);
  }, []);

  const onCancelled = useCallback((data: unknown) => {
    const c = data as SseCallCancelled;
    setIncoming((cur) => (cur && cur.call_id === c.call_id ? null : cur));
    setActive((cur) => {
      if (cur && cur.call_id === c.call_id) {
        setToast({ kind: 'err', text: 'Посетитель завершил звонок.' });
        return null;
      }
      return cur;
    });
  }, []);

  const onAccepted = useCallback((data: unknown) => {
    // Звонок принят (возможно, в другой вкладке) — убираем баннер входящего.
    const c = data as SseCallAccepted;
    setIncoming((cur) => (cur && cur.call_id === c.call_id ? null : cur));
  }, []);

  useSSE(SSE_PATH, {
    events: {
      'call.incoming': onIncoming,
      'call.cancelled': onCancelled,
      'call.accepted': onAccepted,
    },
    onStatus: setSseStatus,
  });

  // --- Опрос статуса устройств раз в 10с ---
  useEffect(() => {
    let stopped = false;
    const load = () => {
      api
        .listDevices()
        .then((r) => {
          if (!stopped) setDevices(r.devices);
        })
        .catch(() => {
          /* индикатор просто не обновится */
        });
    };
    load();
    const t = setInterval(load, 10000);
    return () => {
      stopped = true;
      clearInterval(t);
    };
  }, []);

  // --- Авто-скрытие тоста ---
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 5000);
    return () => clearTimeout(t);
  }, [toast]);

  // --- Действия ---
  const accept = async () => {
    if (!incoming) return;
    setAccepting(true);
    try {
      const res = await api.acceptCall(incoming.call_id);
      setActive({
        ...res,
        call_id: incoming.call_id,
        access_point_label: incoming.access_point_label,
      });
      setIncoming(null);
    } catch (e: unknown) {
      if (e instanceof ApiError && e.code === 'CALL_NOT_FOUND') {
        setToast({ kind: 'err', text: 'Звонок уже недоступен.' });
        setIncoming(null);
      } else {
        setToast({ kind: 'err', text: errorMessage(e) });
      }
    } finally {
      setAccepting(false);
    }
  };

  const reject = async () => {
    if (!incoming) return;
    const id = incoming.call_id;
    setIncoming(null);
    try {
      await api.endCall(id);
    } catch {
      /* всё равно скрыли баннер */
    }
  };

  const hangUp = async () => {
    if (!active) return;
    const id = active.call_id;
    setActive(null);
    try {
      await api.endCall(id);
    } catch {
      /* локально завершили */
    }
  };

  const openDoor = async () => {
    if (!active) return;
    try {
      const res = await api.openDoor(active.call_id);
      setToast({ kind: 'ok', text: `Команда отправлена (${res.request_id})` });
    } catch (e: unknown) {
      if (e instanceof ApiError && e.code === 'DEVICE_OFFLINE') {
        setToast({ kind: 'err', text: 'Устройство offline — дверь нельзя открыть удалённо.' });
      } else if (e instanceof ApiError && e.code === 'CALL_NOT_FOUND') {
        setToast({ kind: 'err', text: 'Сессия звонка не найдена.' });
      } else if (e instanceof ApiError && e.code === 'CALL_NOT_ACCEPTED') {
        setToast({ kind: 'err', text: 'Сначала примите звонок, затем открывайте дверь.' });
      } else {
        setToast({ kind: 'err', text: errorMessage(e) });
      }
    }
  };

  // --- Индикатор устройства ---
  const device = devices?.[0];
  const deviceOnline = device?.status === 'online';

  return (
    <div className="page resident">
      <header className="resident-header">
        <h1>Экран жильца</h1>
        <div className="status-row">
          <span className={`dot ${sseStatus === 'open' ? 'on' : 'off'}`} />
          <span className="muted">
            События: {sseStatus === 'open' ? 'на связи' : 'переподключение…'}
          </span>
          <span className="sep">·</span>
          <span className={`dot ${deviceOnline ? 'on' : 'off'}`} />
          <span className="muted">
            Устройство: {device ? (deviceOnline ? 'online' : 'offline') : '—'}
          </span>
        </div>
      </header>

      {/* Активный звонок */}
      {active ? (
        <section className="card">
          <h2>
            Разговор · <span className="muted">{active.access_point_label}</span>
          </h2>
          <CallRoom
            url={active.livekit_url}
            token={active.resident_token}
            role="resident"
            onDisconnected={() => setActive(null)}
            onError={(e) => setToast({ kind: 'err', text: e.message })}
          />
          <div className="call-actions">
            <button className="btn success big" onClick={openDoor}>
              Открыть дверь
            </button>
            <button className="btn danger" onClick={hangUp}>
              Завершить
            </button>
          </div>
        </section>
      ) : incoming ? (
        /* Входящий звонок */
        <section className="card incoming">
          <div className="pulse">Входящий звонок</div>
          <p className="ap-label">{incoming.access_point_label}</p>
          <div className="call-actions">
            <button className="btn success big" onClick={accept} disabled={accepting}>
              {accepting ? 'Соединение…' : 'Принять'}
            </button>
            <button className="btn danger" onClick={reject} disabled={accepting}>
              Отклонить
            </button>
          </div>
        </section>
      ) : (
        <section className="card center">
          <p className="muted">Ожидание входящих звонков…</p>
        </section>
      )}

      {toast && <div className={`toast ${toast.kind === 'ok' ? 'ok' : 'err'}`}>{toast.text}</div>}
    </div>
  );
}

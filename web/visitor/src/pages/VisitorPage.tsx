import { useEffect, useMemo, useState } from 'react';
import { api, ApiError, errorMessage } from '../api/client';
import type { InitiateResponse, QrParams, ValidateResponse } from '../api/types';
import { CallRoom } from '../components/CallRoom';

type State = 'validating' | 'ready' | 'busy' | 'calling' | 'in_call' | 'ended' | 'error';

/** Читает aid,v,kid,sig из query-строки. Возвращает null, если чего-то нет. */
function readQrParams(): QrParams | null {
  const q = new URLSearchParams(window.location.search);
  const aid = q.get('aid');
  const v = q.get('v');
  const kid = q.get('kid');
  const sig = q.get('sig');
  if (!aid || !v || !kid || !sig) return null;
  return { aid, v, kid, sig };
}

function isDeviceOffline(res: ValidateResponse): boolean {
  return res.device_status === 'offline' || Boolean(res.warning);
}

export function VisitorPage() {
  const qr = useMemo(readQrParams, []);
  const [state, setState] = useState<State>('validating');
  const [validation, setValidation] = useState<ValidateResponse | null>(null);
  const [call, setCall] = useState<InitiateResponse | null>(null);
  const [error, setError] = useState<string>('');

  // Валидация QR на маунте.
  useEffect(() => {
    if (!qr) {
      setError('Ссылка неполная: отсутствуют параметры QR (aid, v, kid, sig).');
      setState('error');
      return;
    }
    let active = true;
    setState('validating');
    api
      .validateQr(qr)
      .then((res) => {
        if (!active) return;
        setValidation(res);
        setState('ready');
      })
      .catch((e: unknown) => {
        if (!active) return;
        if (e instanceof ApiError && e.code === 'INVALID_QR') {
          setError('QR-код недействителен. Обратитесь к жильцу или в управляющую компанию.');
        } else {
          setError(errorMessage(e));
        }
        setState('error');
      });
    return () => {
      active = false;
    };
  }, [qr]);

  const startCall = async () => {
    if (!qr) return;
    setState('calling');
    setError('');
    try {
      const res = await api.initiateCall(qr);
      setCall(res);
      setState('in_call');
    } catch (e: unknown) {
      if (e instanceof ApiError && e.code === 'CALL_IN_PROGRESS') {
        setError('В эту квартиру уже идёт другой звонок. Попробуйте немного позже.');
        setState('busy');
      } else {
        setError(errorMessage(e));
        setState('error');
      }
    }
  };

  const endCall = async () => {
    if (call) {
      try {
        await api.endCall(call.call_id);
      } catch {
        // Завершение локально всё равно выполняем.
      }
    }
    setCall(null);
    setState('ended');
  };

  // --- Рендер по состоянию ---

  if (state === 'validating') {
    return (
      <div className="page visitor">
        <div className="card center">
          <div className="spinner" />
          <p>Проверяем QR-код…</p>
        </div>
      </div>
    );
  }

  if (state === 'error') {
    return (
      <div className="page visitor">
        <div className="card">
          <div className="banner error">{error || 'Произошла ошибка.'}</div>
          <p className="hint">Звонок недоступен. Проверьте ссылку QR-кода.</p>
        </div>
      </div>
    );
  }

  if (state === 'ended') {
    return (
      <div className="page visitor">
        <div className="card center">
          <h2>Звонок завершён</h2>
          <button className="btn primary" onClick={() => setState('ready')}>
            Позвонить снова
          </button>
        </div>
      </div>
    );
  }

  if (state === 'in_call' && call) {
    return (
      <div className="page visitor call">
        <header className="call-header">
          <div>
            <strong>{validation?.access_point.label ?? 'Звонок'}</strong>
            <span className="muted"> · кв. {validation?.apartment.number}</span>
          </div>
        </header>
        <CallRoom
          url={call.livekit_url}
          token={call.visitor_token}
          role="visitor"
          onDisconnected={() => setState('ended')}
          onError={(e) => setError(e.message)}
        />
        <div className="call-actions">
          <p className="hint">Ожидайте ответа жильца…</p>
          <button className="btn danger" onClick={endCall}>
            Завершить
          </button>
        </div>
        {error && <div className="banner error small">{error}</div>}
      </div>
    );
  }

  // state === 'ready' | 'calling' | 'busy'
  const offline = validation ? isDeviceOffline(validation) : false;
  return (
    <div className="page visitor">
      <div className="card">
        <h1 className="ap-label">{validation?.access_point.label}</h1>
        <p className="apartment">Квартира {validation?.apartment.number}</p>

        {offline && (
          <div className="banner warning">
            {validation?.warning ??
              'Устройство временно недоступно. Вы можете позвонить жильцу — он откроет дверь, когда связь восстановится.'}
          </div>
        )}

        {state === 'busy' && <div className="banner error">{error}</div>}

        <button
          className="btn primary big"
          onClick={startCall}
          disabled={state === 'calling'}
        >
          {state === 'calling' ? 'Соединение…' : 'Позвонить'}
        </button>

        <p className="hint">
          Нажмите «Позвонить», чтобы связаться с жильцом по видео. Понадобится доступ к камере и
          микрофону.
        </p>
      </div>
    </div>
  );
}

import { useCallback, useEffect, useState } from 'react';
import { api, ApiError, errorMessage } from '../api/client';
import type { SchedulePoint } from '../api/types';

const DOW = ['Вс', 'Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб'];
const DEFAULT_TZ = 'Asia/Almaty';

function mapErr(e: unknown): string {
  if (e instanceof ApiError && e.code === 'VALIDATION_ERROR') return e.message || 'Проверьте данные.';
  return errorMessage(e);
}

/** Форма добавления окна авто-открытия для точки. */
function AddSchedule({ publicId, onAdded }: { publicId: string; onAdded: () => void }) {
  const [dow, setDow] = useState(1);
  const [opens, setOpens] = useState('08:00');
  const [closes, setCloses] = useState('20:00');
  const [tz, setTz] = useState(DEFAULT_TZ);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const add = async () => {
    setBusy(true);
    setErr(null);
    try {
      await api.adminCreateSchedule(publicId, { dow, opens, closes, timezone: tz.trim() });
      onAdded();
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="sched-add">
      <select value={dow} disabled={busy} onChange={(e) => setDow(Number(e.target.value))}>
        {DOW.map((d, i) => <option key={i} value={i}>{d}</option>)}
      </select>
      <input type="time" value={opens} disabled={busy} onChange={(e) => setOpens(e.target.value)} />
      <span>–</span>
      <input type="time" value={closes} disabled={busy} onChange={(e) => setCloses(e.target.value)} />
      <input className="tz-input" value={tz} disabled={busy} onChange={(e) => setTz(e.target.value)} title="Таймзона (IANA)" />
      <button className="btn primary small" onClick={add} disabled={busy}>Добавить</button>
      {err && <span className="err-text" style={{ margin: 0 }}>{err}</span>}
    </div>
  );
}

/** Экран «Калитки/Шлагбаумы»: точки + их расписания авто-открытия (время работы). */
export function SchedulesPanel({ onClose }: { onClose: () => void }) {
  const [points, setPoints] = useState<SchedulePoint[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [filterType, setFilterType] = useState<'all' | 'gate' | 'barrier'>('all');

  const load = useCallback(async () => {
    setErr(null);
    try {
      const r = await api.adminSchedulePoints();
      setPoints(r.points);
    } catch (e) {
      setErr(mapErr(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const del = async (id: string) => {
    try {
      await api.adminDeleteSchedule(id);
      await load();
    } catch (e) {
      setErr(mapErr(e));
    }
  };

  const shown = points.filter((p) => filterType === 'all' || p.type === filterType);

  return (
    <div className="card admin-card wide">
      <div className="header-top">
        <h2>Калитки и шлагбаумы — время работы</h2>
        <button className="btn ghost small" onClick={onClose}>Закрыть</button>
      </div>
      <div className="tabs" style={{ maxWidth: 320 }}>
        {(['all', 'gate', 'barrier'] as const).map((t) => (
          <button key={t} className={`tab ${filterType === t ? 'active' : ''}`} onClick={() => setFilterType(t)}>
            {t === 'all' ? 'Все' : t === 'gate' ? 'Калитки' : 'Шлагбаумы'}
          </button>
        ))}
      </div>
      {err && <p className="err-text">{err}</p>}
      {shown.length === 0 && <p className="muted">Нет точек.</p>}
      {shown.map((p) => (
        <div key={p.public_id} className="sched-point">
          <div className="sched-point-head">
            {p.label} <span className="hint">({p.type === 'gate' ? 'калитка' : 'шлагбаум'})</span>
          </div>
          {p.schedules.length === 0 && <p className="hint" style={{ margin: '4px 0' }}>Расписаний нет — точка открывается только по запросу.</p>}
          {p.schedules.map((s) => (
            <div key={s.id} className="sched-row">
              <span className="chip">{DOW[s.dow]} {s.opens}–{s.closes}</span>
              <span className="hint">{s.timezone}</span>
              <button className="btn ghost small" onClick={() => del(s.id)}>Удалить</button>
            </div>
          ))}
          <AddSchedule publicId={p.public_id} onAdded={load} />
        </div>
      ))}
      <p className="hint" style={{ marginTop: 12 }}>
        В заданное окно точка удерживается открытой автоматически. Отказ сервера — точка закрывается сама (fail-secure).
      </p>
    </div>
  );
}

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, ApiError, errorMessage } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import type { CatalogResponse, InviteInfo, ResidentInfo } from '../api/types';

function mapErr(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'FORBIDDEN') return 'Объект принадлежит другой УК или недоступен.';
    if (e.code === 'VALIDATION_ERROR') return 'Проверьте введённые данные.';
    if (e.code === 'RATE_LIMIT') return 'Слишком много запросов — попробуйте позже.';
  }
  return errorMessage(e);
}

/** Блок с выданной инвайт-ссылкой (доставка — сам пользователь: копирует и шлёт). */
function InviteBlock({ invite }: { invite: InviteInfo }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(invite.url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard может быть недоступен (http) — ссылка всё равно видна */
    }
  };
  return (
    <div className="banner warning small invite-block">
      <p className="muted" style={{ margin: '0 0 6px' }}>
        Инвайт-ссылка — скопируйте и отправьте пользователю (SMS / мессенджер):
      </p>
      <code className="invite-url">{invite.url}</code>
      <div className="invite-actions">
        <button className="btn ghost small" onClick={copy}>
          {copied ? 'Скопировано' : 'Копировать'}
        </button>
        <span className="hint">до {new Date(invite.expires_at).toLocaleString('ru-RU')}</span>
      </div>
    </div>
  );
}

/**
 * Форма «Создать владельца»: выбор дом→подъезд→квартира из каталога + ФИО +
 * телефон + чекбоксы доступов на калитки/шлагбаумы (композитный инвайт: одна
 * ссылка несёт квартиру и выбранные точки).
 */
function CreateOwnerCard({ catalog }: { catalog: CatalogResponse | null }) {
  const [buildingId, setBuildingId] = useState('');
  const [entranceKey, setEntranceKey] = useState(''); // индекс подъезда в доме
  const [apartmentId, setApartmentId] = useState('');
  const [fullName, setFullName] = useState('');
  const [phone, setPhone] = useState('');
  const [points, setPoints] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [invite, setInvite] = useState<InviteInfo | null>(null);

  const building = useMemo(
    () => catalog?.buildings.find((b) => b.id === buildingId) ?? null,
    [catalog, buildingId],
  );
  const entrance = useMemo(
    () => building?.entrances[Number(entranceKey)] ?? null,
    [building, entranceKey],
  );

  const togglePoint = (publicId: string) => {
    setPoints((prev) => {
      const next = new Set(prev);
      if (next.has(publicId)) next.delete(publicId);
      else next.add(publicId);
      return next;
    });
  };

  const reset = () => {
    setEntranceKey('');
    setApartmentId('');
  };

  const submit = async () => {
    setBusy(true);
    setErr(null);
    setInvite(null);
    try {
      const r = await api.adminCreateOwner(
        apartmentId,
        phone.trim(),
        fullName.trim(),
        Array.from(points),
      );
      setInvite(r.invite);
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  const canSubmit = !!apartmentId && !!phone.trim() && !!fullName.trim() && !busy;

  return (
    <div className="card admin-card">
      <h2>Создать владельца</h2>
      <p className="hint">
        Назначает роль владельца на квартиру и (опционально) выдаёт доступ на калитки/шлагбаумы —
        всё одной инвайт-ссылкой.
      </p>
      <div className="form">
        <label>
          ФИО владельца
          <input value={fullName} disabled={busy} onChange={(e) => setFullName(e.target.value)} placeholder="Иванов Иван Иванович" />
        </label>
        <label>
          Телефон
          <input type="tel" value={phone} disabled={busy} onChange={(e) => setPhone(e.target.value)} placeholder="+7…" />
        </label>

        <label>
          Дом
          <select
            value={buildingId}
            disabled={busy || !catalog}
            onChange={(e) => {
              setBuildingId(e.target.value);
              reset();
            }}
          >
            <option value="">— выберите дом —</option>
            {catalog?.buildings.map((b) => (
              <option key={b.id} value={b.id}>{b.address}</option>
            ))}
          </select>
        </label>

        <label>
          Подъезд
          <select
            value={entranceKey}
            disabled={busy || !building}
            onChange={(e) => {
              setEntranceKey(e.target.value);
              setApartmentId('');
            }}
          >
            <option value="">— выберите подъезд —</option>
            {building?.entrances.map((ent, i) => (
              <option key={ent.id || `none-${i}`} value={String(i)}>
                {ent.id ? `Подъезд ${ent.number}` : 'Без подъезда'}
              </option>
            ))}
          </select>
        </label>

        <label>
          Квартира
          <select
            value={apartmentId}
            disabled={busy || !entrance}
            onChange={(e) => setApartmentId(e.target.value)}
          >
            <option value="">— выберите квартиру —</option>
            {entrance?.apartments.map((a) => (
              <option key={a.id} value={a.id}>кв. {a.number}</option>
            ))}
          </select>
        </label>

        {catalog && catalog.points.length > 0 && (
          <fieldset className="points-fieldset">
            <legend>Доступы на калитки / шлагбаумы</legend>
            {catalog.points.map((p) => (
              <label key={p.public_id} className="checkbox-row">
                <input
                  type="checkbox"
                  checked={points.has(p.public_id)}
                  disabled={busy}
                  onChange={() => togglePoint(p.public_id)}
                />
                <span>{p.label} <span className="hint">({p.type})</span></span>
              </label>
            ))}
          </fieldset>
        )}

        <button className="btn primary" onClick={submit} disabled={!canSubmit}>
          {busy ? 'Создание…' : 'Создать и выдать ссылку'}
        </button>
      </div>
      {err && <p className="err-text">{err}</p>}
      {invite && <InviteBlock invite={invite} />}
    </div>
  );
}

/** Форма «Выдать доступ»: грант на калитку/шлагбаум — сразу или через инвайт. */
function CreateGrantCard({ catalog }: { catalog: CatalogResponse | null }) {
  const [publicId, setPublicId] = useState('');
  const [fullName, setFullName] = useState('');
  const [phone, setPhone] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [invite, setInvite] = useState<InviteInfo | null>(null);
  const [granted, setGranted] = useState(false);

  const submit = async () => {
    setBusy(true);
    setErr(null);
    setInvite(null);
    setGranted(false);
    try {
      const r = await api.adminCreateGrant(publicId, phone.trim(), fullName.trim());
      if (r.granted) setGranted(true);
      else if (r.invite) setInvite(r.invite);
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  const canSubmit = !!publicId && !!phone.trim() && !!fullName.trim() && !busy;

  return (
    <div className="card admin-card">
      <h2>Выдать доступ на калитку / шлагбаум</h2>
      <p className="hint">
        Если пользователь уже в вашей УК — грант выдаётся сразу; иначе выпускается инвайт-ссылка.
      </p>
      <div className="form">
        <label>
          ФИО пользователя
          <input value={fullName} disabled={busy} onChange={(e) => setFullName(e.target.value)} placeholder="Иванов Иван Иванович" />
        </label>
        <label>
          Телефон
          <input type="tel" value={phone} disabled={busy} onChange={(e) => setPhone(e.target.value)} placeholder="+7…" />
        </label>
        <label>
          Точка
          <select value={publicId} disabled={busy || !catalog} onChange={(e) => setPublicId(e.target.value)}>
            <option value="">— выберите калитку/шлагбаум —</option>
            {catalog?.points.map((p) => (
              <option key={p.public_id} value={p.public_id}>{p.label} ({p.type})</option>
            ))}
          </select>
        </label>
        <button className="btn primary" onClick={submit} disabled={!canSubmit}>
          {busy ? 'Выдача…' : 'Выдать доступ'}
        </button>
      </div>
      {err && <p className="err-text">{err}</p>}
      {granted && <p className="ok-text">Доступ выдан существующему пользователю.</p>}
      {invite && <InviteBlock invite={invite} />}
    </div>
  );
}

/** Список жильцов/владельцев УК (scoped по mc на сервере). */
function ResidentsCard() {
  const [rows, setRows] = useState<ResidentInfo[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(async () => {
    setBusy(true);
    setErr(null);
    try {
      const r = await api.adminListResidents();
      setRows(r.residents);
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  return (
    <div className="card admin-card wide">
      <div className="header-top">
        <h2>Жильцы и владельцы</h2>
        <button className="btn ghost small" onClick={load} disabled={busy}>
          {busy ? 'Загрузка…' : 'Обновить'}
        </button>
      </div>
      {err && <p className="err-text">{err}</p>}
      {!err && rows.length === 0 && !busy && <p className="muted">Пока никого нет.</p>}
      {rows.length > 0 && (
        <div className="res-list">
          {rows.map((r) => (
            <div key={r.user_id} className="res-row">
              <div className="res-main">
                <span className="res-name">{r.full_name || '—'}</span>
                <span className="res-phone">{r.phone || '—'}</span>
                <span className={`tag tag-${r.kind}`}>{r.kind}</span>
              </div>
              <div className="res-meta">
                {r.apartments.map((a) => (
                  <span key={a.id} className="chip">
                    кв. {a.number} · {a.role}
                  </span>
                ))}
                {r.grants.map((g) => (
                  <span key={g.public_id} className="chip chip-grant">
                    {g.label}
                  </span>
                ))}
                {r.apartments.length === 0 && r.grants.length === 0 && (
                  <span className="hint">нет привязок</span>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export function AdminPage() {
  const { user, logout } = useAuth();
  const nav = useNavigate();
  const [catalog, setCatalog] = useState<CatalogResponse | null>(null);
  const [catalogErr, setCatalogErr] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const c = await api.adminCatalog();
        if (!cancelled) setCatalog(c);
      } catch (e) {
        if (!cancelled) setCatalogErr(mapErr(e));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const doLogout = async () => {
    await logout();
    nav('/login', { replace: true });
  };

  return (
    <div className="page admin">
      <div className="admin-header header-top">
        <div>
          <h1 style={{ margin: 0 }}>УК-консоль</h1>
          <p className="hint" style={{ margin: '2px 0 0' }}>
            {user?.mc_id ? `mc: ${user.mc_id}` : ''}
          </p>
        </div>
        <button className="btn ghost small" onClick={doLogout}>
          Выйти
        </button>
      </div>

      {catalogErr && <p className="err-text">Каталог не загрузился: {catalogErr}</p>}

      <div className="admin-grid">
        <CreateOwnerCard catalog={catalog} />
        <CreateGrantCard catalog={catalog} />
        <ResidentsCard />
      </div>

      <p className="hint" style={{ marginTop: 20, textAlign: 'center' }}>
        Интерфейс жильца/владельца (приём инвайта, открытие калиток) — в мобильном приложении.
      </p>
    </div>
  );
}

import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, ApiError, errorMessage } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import type { InviteInfo, ResidentInfo } from '../api/types';

// Демо-фикстуры (architecture.md §5): квартира и калитка двора для быстрой проверки.
const DEMO_APARTMENT = '33333333-3333-3333-3333-333333333333';
const DEMO_GATE = 'eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee';

function mapErr(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'FORBIDDEN') return 'Объект принадлежит другой УК или недоступен.';
    if (e.code === 'VALIDATION_ERROR') return 'Проверьте введённые данные (квартира/точка не найдена?).';
    if (e.code === 'RATE_LIMIT') return 'Слишком много запросов — попробуйте позже.';
  }
  return errorMessage(e);
}

/** Блок с выданной инвайт-ссылкой (мок доставки: ссылку копируют и передают вручную). */
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
        Инвайт-ссылка (передайте пользователю):
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

/** Форма «Создать владельца»: назначает роль owner на квартиру + инвайт-ссылка. */
function CreateOwnerCard() {
  const [apartmentId, setApartmentId] = useState(DEMO_APARTMENT);
  const [phone, setPhone] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [invite, setInvite] = useState<InviteInfo | null>(null);

  const submit = async () => {
    setBusy(true);
    setErr(null);
    setInvite(null);
    try {
      const r = await api.adminCreateOwner(apartmentId.trim(), phone.trim());
      setInvite(r.invite);
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card admin-card">
      <h2>Создать владельца</h2>
      <p className="hint">Назначает роль владельца на квартиру вашей УК и выпускает инвайт-ссылку.</p>
      <div className="form">
        <label>
          ID квартиры
          <input value={apartmentId} disabled={busy} onChange={(e) => setApartmentId(e.target.value)} />
        </label>
        <label>
          Телефон владельца
          <input
            type="tel"
            value={phone}
            disabled={busy}
            onChange={(e) => setPhone(e.target.value)}
            placeholder="+7…"
          />
        </label>
        <button className="btn primary" onClick={submit} disabled={busy || !apartmentId || !phone}>
          {busy ? 'Создание…' : 'Создать и выдать ссылку'}
        </button>
      </div>
      {err && <p className="err-text">{err}</p>}
      {invite && <InviteBlock invite={invite} />}
    </div>
  );
}

/** Форма «Выдать доступ»: грант на калитку/шлагбаум — сразу или через инвайт. */
function CreateGrantCard() {
  const [publicId, setPublicId] = useState(DEMO_GATE);
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
      const r = await api.adminCreateGrant(publicId.trim(), phone.trim());
      if (r.granted) {
        setGranted(true);
      } else if (r.invite) {
        setInvite(r.invite);
      }
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card admin-card">
      <h2>Выдать доступ на калитку / шлагбаум</h2>
      <p className="hint">
        Если пользователь уже в вашей УК — грант выдаётся сразу; иначе выпускается инвайт-ссылка.
      </p>
      <div className="form">
        <label>
          Публичный ID точки (калитка/шлагбаум)
          <input value={publicId} disabled={busy} onChange={(e) => setPublicId(e.target.value)} />
        </label>
        <label>
          Телефон пользователя
          <input
            type="tel"
            value={phone}
            disabled={busy}
            onChange={(e) => setPhone(e.target.value)}
            placeholder="+7…"
          />
        </label>
        <button className="btn primary" onClick={submit} disabled={busy || !publicId || !phone}>
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
            {user?.id ? `mc: ${user.mc_id ?? '—'}` : ''}
          </p>
        </div>
        <button className="btn ghost small" onClick={doLogout}>
          Выйти
        </button>
      </div>

      <div className="admin-grid">
        <CreateOwnerCard />
        <CreateGrantCard />
        <ResidentsCard />
      </div>

      <p className="hint" style={{ marginTop: 20, textAlign: 'center' }}>
        Интерфейс жильца/владельца (приём инвайта, открытие калиток) — в мобильном приложении.
      </p>
    </div>
  );
}

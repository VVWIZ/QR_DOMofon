import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, ApiError, errorMessage } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import { SchedulesPanel } from '../components/SchedulesPanel';
import type {
  AdminSite,
  CatalogResponse,
  InviteInfo,
  MatrixResident,
  MatrixResponse,
} from '../api/types';

function mapErr(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'FORBIDDEN') return 'Недостаточно прав или объект чужой УК.';
    if (e.code === 'VALIDATION_ERROR') return e.message || 'Проверьте данные.';
    if (e.code === 'RATE_LIMIT') return 'Слишком часто — попробуйте позже.';
  }
  return errorMessage(e);
}

/** Инвайт-ссылка (создатель сам пересылает адресату). */
function InviteBlock({ invite }: { invite: InviteInfo }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(invite.url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard недоступен (http) */
    }
  };
  return (
    <div className="banner warning small invite-block">
      <p className="muted" style={{ margin: '0 0 6px' }}>Инвайт-ссылка — скопируйте и отправьте:</p>
      <code className="invite-url">{invite.url}</code>
      <div className="invite-actions">
        <button className="btn ghost small" onClick={copy}>{copied ? 'Скопировано' : 'Копировать'}</button>
        <span className="hint">до {new Date(invite.expires_at).toLocaleString('ru-RU')}</span>
      </div>
    </div>
  );
}

/** Форма создания владельца (композитный инвайт: квартира + доступы на точки). */
function CreateOwnerForm({ catalog, onClose }: { catalog: CatalogResponse | null; onClose: () => void }) {
  const [buildingId, setBuildingId] = useState('');
  const [entranceKey, setEntranceKey] = useState('');
  const [apartmentId, setApartmentId] = useState('');
  const [fullName, setFullName] = useState('');
  const [phone, setPhone] = useState('');
  const [points, setPoints] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [invite, setInvite] = useState<InviteInfo | null>(null);

  const building = useMemo(() => catalog?.buildings.find((b) => b.id === buildingId) ?? null, [catalog, buildingId]);
  const entrance = useMemo(() => building?.entrances[Number(entranceKey)] ?? null, [building, entranceKey]);

  const togglePoint = (id: string) =>
    setPoints((prev) => {
      const n = new Set(prev);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });

  const submit = async () => {
    setBusy(true);
    setErr(null);
    setInvite(null);
    try {
      const r = await api.adminCreateOwner(apartmentId, phone.trim(), fullName.trim(), Array.from(points));
      setInvite(r.invite);
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  const canSubmit = !!apartmentId && !!phone.trim() && !!fullName.trim() && !busy;

  return (
    <div className="card admin-card wide">
      <div className="header-top">
        <h2>Создать владельца</h2>
        <button className="btn ghost small" onClick={onClose}>Закрыть</button>
      </div>
      <div className="form">
        <label>ФИО<input value={fullName} disabled={busy} onChange={(e) => setFullName(e.target.value)} placeholder="Иванов Иван Иванович" /></label>
        <label>Телефон<input type="tel" value={phone} disabled={busy} onChange={(e) => setPhone(e.target.value)} placeholder="+7…" /></label>
        <label>Дом
          <select value={buildingId} disabled={busy || !catalog} onChange={(e) => { setBuildingId(e.target.value); setEntranceKey(''); setApartmentId(''); }}>
            <option value="">— дом —</option>
            {catalog?.buildings.map((b) => <option key={b.id} value={b.id}>{b.address}</option>)}
          </select>
        </label>
        <label>Подъезд
          <select value={entranceKey} disabled={busy || !building} onChange={(e) => { setEntranceKey(e.target.value); setApartmentId(''); }}>
            <option value="">— подъезд —</option>
            {building?.entrances.map((ent, i) => <option key={ent.id || i} value={String(i)}>{ent.id ? `Подъезд ${ent.number}` : 'Без подъезда'}</option>)}
          </select>
        </label>
        <label>Квартира
          <select value={apartmentId} disabled={busy || !entrance} onChange={(e) => setApartmentId(e.target.value)}>
            <option value="">— квартира —</option>
            {entrance?.apartments.map((a) => <option key={a.id} value={a.id}>кв. {a.number}</option>)}
          </select>
        </label>
        {catalog && catalog.points.length > 0 && (
          <fieldset className="points-fieldset">
            <legend>Доступы на калитки / шлагбаумы</legend>
            {catalog.points.map((p) => (
              <label key={p.public_id} className="checkbox-row">
                <input type="checkbox" checked={points.has(p.public_id)} disabled={busy} onChange={() => togglePoint(p.public_id)} />
                <span>{p.label} <span className="hint">({p.type})</span></span>
              </label>
            ))}
          </fieldset>
        )}
        <button className="btn primary" onClick={submit} disabled={!canSubmit}>{busy ? 'Создание…' : 'Создать и выдать ссылку'}</button>
      </div>
      {err && <p className="err-text">{err}</p>}
      {invite && <InviteBlock invite={invite} />}
    </div>
  );
}

/** Матрица доступа объекта: владельцы × точки с чекбоксами; раскрытие → жильцы. */
function MatrixView({ siteId }: { siteId: string }) {
  const [data, setData] = useState<MatrixResponse | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [filter, setFilter] = useState('');
  const [typeFilter, setTypeFilter] = useState<'all' | 'gate' | 'barrier'>('all');
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [residents, setResidents] = useState<Record<string, MatrixResident[]>>({});
  const [pending, setPending] = useState<Set<string>>(new Set());
  const [toast, setToast] = useState<string | null>(null);

  const load = useCallback(async () => {
    setErr(null);
    try {
      setData(await api.adminSiteMatrix(siteId));
    } catch (e) {
      setErr(mapErr(e));
    }
  }, [siteId]);

  useEffect(() => {
    setExpanded(new Set());
    setResidents({});
    void load();
  }, [load]);

  const points = useMemo(
    () => (data?.points ?? []).filter((p) => typeFilter === 'all' || p.type === typeFilter),
    [data, typeFilter],
  );
  const owners = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return (data?.owners ?? []).filter((o) =>
      !q || o.full_name.toLowerCase().includes(q) || o.phone.includes(q) || o.apartments.some((a) => a.number.includes(q)),
    );
  }, [data, filter]);

  // Оптимистичный тогл гранта: userId, publicId, hasNow (в UI-состоянии).
  const toggle = async (userId: string, publicId: string, hasNow: boolean, scope: 'owner' | { apartmentId: string }) => {
    const key = `${userId}|${publicId}`;
    if (pending.has(key)) return;
    setPending((p) => new Set(p).add(key));
    // локальное оптимистичное изменение
    const apply = (has: boolean) => {
      if (scope === 'owner') {
        setData((d) => d && { ...d, owners: d.owners.map((o) => o.user_id === userId ? { ...o, grants: setGrant(o.grants, publicId, has) } : o) });
      } else {
        setResidents((r) => ({ ...r, [scope.apartmentId]: (r[scope.apartmentId] ?? []).map((x) => x.user_id === userId ? { ...x, grants: setGrant(x.grants, publicId, has) } : x) }));
      }
    };
    apply(!hasNow);
    try {
      if (hasNow) await api.adminRevoke(userId, publicId);
      else await api.adminGrant(userId, publicId);
    } catch (e) {
      apply(hasNow); // откат
      setToast(mapErr(e));
      setTimeout(() => setToast(null), 3000);
    } finally {
      setPending((p) => { const n = new Set(p); n.delete(key); return n; });
    }
  };

  const expand = async (owner: MatrixResponse['owners'][number]) => {
    const open = new Set(expanded);
    if (open.has(owner.user_id)) {
      open.delete(owner.user_id);
      setExpanded(open);
      return;
    }
    open.add(owner.user_id);
    setExpanded(open);
    for (const a of owner.apartments) {
      if (residents[a.id]) continue;
      try {
        const r = await api.adminApartmentResidents(a.id);
        setResidents((prev) => ({ ...prev, [a.id]: r.residents }));
      } catch {
        /* строка просто не раскроется по этой квартире */
      }
    }
  };

  const cell = (userId: string, grants: string[], publicId: string, scope: 'owner' | { apartmentId: string }) => {
    const has = grants.includes(publicId);
    const key = `${userId}|${publicId}`;
    return (
      <td key={publicId} className="matrix-cell">
        <input type="checkbox" checked={has} disabled={pending.has(key)} onChange={() => toggle(userId, publicId, has, scope)} />
      </td>
    );
  };

  if (err) return <div className="card admin-card wide"><p className="err-text">{err}</p></div>;
  if (!data) return <div className="card admin-card wide"><div className="spinner" /></div>;

  return (
    <div className="card admin-card wide">
      <div className="matrix-toolbar">
        <input className="matrix-filter" placeholder="Фильтр: ФИО, телефон, № квартиры" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <div className="tabs">
          {(['all', 'gate', 'barrier'] as const).map((t) => (
            <button key={t} className={`tab ${typeFilter === t ? 'active' : ''}`} onClick={() => setTypeFilter(t)}>
              {t === 'all' ? 'Все' : t === 'gate' ? 'Калитки' : 'Шлагбаумы'}
            </button>
          ))}
        </div>
        <button className="btn ghost small" onClick={load}>Обновить</button>
      </div>

      <div className="matrix-scroll">
        <table className="matrix">
          <thead>
            <tr>
              <th className="col-apt">Квартира</th>
              <th className="col-owner">Собственник</th>
              {points.map((p) => <th key={p.public_id} className="col-point">{p.label}<div className="hint">{p.type === 'gate' ? 'калитка' : 'шлагбаум'}</div></th>)}
            </tr>
          </thead>
          <tbody>
            {owners.length === 0 && <tr><td colSpan={2 + points.length} className="muted">Нет владельцев.</td></tr>}
            {owners.map((o) => {
              const isOpen = expanded.has(o.user_id);
              return (
                <Fragment key={o.user_id}>
                  <tr className={`owner-row ${isOpen ? 'open' : ''}`}>
                    <td className="col-apt">{o.apartments.map((a) => `кв. ${a.number}`).join(', ') || '—'}</td>
                    <td className="col-owner">
                      <button className="expand-btn" onClick={() => expand(o)} title="Показать жильцов">{isOpen ? '▾' : '▸'}</button>
                      <span className="res-name">{o.full_name || '—'}</span>
                      <span className="res-phone">{o.phone}</span>
                    </td>
                    {points.map((p) => cell(o.user_id, o.grants, p.public_id, 'owner'))}
                  </tr>
                  {isOpen && o.apartments.flatMap((a) => (residents[a.id] ?? []).map((res) => (
                    <tr key={`${o.user_id}-${res.user_id}`} className="resident-row">
                      <td className="col-apt hint">кв. {a.number} · жилец</td>
                      <td className="col-owner">
                        <span className="res-name" style={{ paddingLeft: 22 }}>{res.full_name || '—'}</span>
                        <span className="res-phone">{res.phone}</span>
                      </td>
                      {points.map((p) => cell(res.user_id, res.grants, p.public_id, { apartmentId: a.id }))}
                    </tr>
                  )))}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
      {toast && <div className="toast err">{toast}</div>}
    </div>
  );
}

/** Заменяет наличие гранта в массиве. */
function setGrant(grants: string[], publicId: string, has: boolean): string[] {
  const s = new Set(grants);
  has ? s.add(publicId) : s.delete(publicId);
  return Array.from(s);
}

export function AdminPage() {
  const { user, logout } = useAuth();
  const nav = useNavigate();
  const [catalog, setCatalog] = useState<CatalogResponse | null>(null);
  const [sites, setSites] = useState<AdminSite[]>([]);
  const [siteId, setSiteId] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [showSchedules, setShowSchedules] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const [c, s] = await Promise.all([api.adminCatalog(), api.adminListSites()]);
        if (cancelled) return;
        setCatalog(c);
        setSites(s.sites);
        if (s.sites[0]) setSiteId(s.sites[0].id);
      } catch (e) {
        if (!cancelled) setErr(mapErr(e));
      }
    })();
    return () => { cancelled = true; };
  }, []);

  const doLogout = async () => {
    await logout();
    nav('/login', { replace: true });
  };

  return (
    <div className="page admin sys">
      <div className="admin-header header-top">
        <div className="mc-title">
          <h1 style={{ margin: 0 }}>УК-консоль</h1>
          <p className="hint" style={{ margin: '2px 0 0' }}>{user?.mc_id ? `mc: ${user.mc_id}` : ''}</p>
        </div>
        <div className="admin-actions">
          <select className="site-select" value={siteId} onChange={(e) => setSiteId(e.target.value)}>
            {sites.length === 0 && <option value="">— нет объектов —</option>}
            {sites.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
          </select>
          <button className="btn primary small" onClick={() => setShowCreate((v) => !v)}>+ Владелец</button>
          <button className="btn ghost small" onClick={() => setShowSchedules((v) => !v)}>Калитки/Шлагбаумы</button>
          <button className="btn ghost small" onClick={doLogout}>Выйти</button>
        </div>
      </div>

      {err && <p className="err-text">{err}</p>}

      {showCreate && <div style={{ marginBottom: 16 }}><CreateOwnerForm catalog={catalog} onClose={() => setShowCreate(false)} /></div>}

      {showSchedules && <div style={{ marginBottom: 16 }}><SchedulesPanel onClose={() => setShowSchedules(false)} /></div>}

      {siteId ? <MatrixView key={siteId} siteId={siteId} /> : <p className="muted">Создайте объект в платформенной админке.</p>}

      <p className="hint" style={{ marginTop: 20, textAlign: 'center' }}>
        Интерфейс жильца/владельца (приём инвайта, открытие точек) — в мобильном приложении.
      </p>
    </div>
  );
}

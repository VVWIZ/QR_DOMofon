import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, ApiError, errorMessage } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import type { SysMC, SysSite } from '../api/types';

function mapErr(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'VALIDATION_ERROR') return e.message || 'Проверьте данные.';
    if (e.code === 'FORBIDDEN') return 'Недостаточно прав.';
  }
  return errorMessage(e);
}

/** Список УК + создание. */
function CompaniesPanel({
  mcs,
  selected,
  onSelect,
  onCreated,
}: {
  mcs: SysMC[];
  selected: string | null;
  onSelect: (id: string) => void;
  onCreated: () => void;
}) {
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const create = async () => {
    setBusy(true);
    setErr(null);
    try {
      await api.sysCreateMC(name.trim());
      setName('');
      onCreated();
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card admin-card">
      <h2>Управляющие компании</h2>
      <div className="mc-list">
        {mcs.map((m) => (
          <button
            key={m.id}
            className={`mc-row ${selected === m.id ? 'active' : ''}`}
            onClick={() => onSelect(m.id)}
          >
            <span className="res-name">{m.name}</span>
            <span className="hint">{m.sites} об. · {m.buildings} д.</span>
          </button>
        ))}
        {mcs.length === 0 && <p className="muted">Пока нет УК.</p>}
      </div>
      <div className="form" style={{ marginTop: 12 }}>
        <label>
          Новая УК
          <input value={name} disabled={busy} onChange={(e) => setName(e.target.value)} placeholder="Название УК" />
        </label>
        <button className="btn primary" onClick={create} disabled={busy || !name.trim()}>
          {busy ? 'Создание…' : 'Создать УК'}
        </button>
      </div>
      {err && <p className="err-text">{err}</p>}
    </div>
  );
}

/** Дерево объектов выбранной УК + формы создания объекта/дома/подъезда/админа. */
function CompanyDetail({ mcId, mcName }: { mcId: string; mcName: string }) {
  const [sites, setSites] = useState<SysSite[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // формы
  const [siteName, setSiteName] = useState('');
  const [siteAddr, setSiteAddr] = useState('');
  const [siteKind, setSiteKind] = useState('complex');
  const [bSite, setBSite] = useState('');
  const [bAddr, setBAddr] = useState('');
  const [eBuilding, setEBuilding] = useState('');
  const [eNum, setENum] = useState('');
  const [aEmail, setAEmail] = useState('');
  const [aName, setAName] = useState('');
  const [aPass, setAPass] = useState('');
  const [otpauth, setOtpauth] = useState<string | null>(null);

  const load = useCallback(async () => {
    setErr(null);
    try {
      const c = await api.sysCatalog(mcId);
      setSites(c.sites);
    } catch (e) {
      setErr(mapErr(e));
    }
  }, [mcId]);

  useEffect(() => {
    void load();
  }, [load]);

  const buildings = useMemo(
    () => sites.flatMap((s) => s.buildings.map((b) => ({ ...b, siteName: s.name }))),
    [sites],
  );

  const run = async (fn: () => Promise<unknown>, after?: () => void) => {
    setBusy(true);
    setErr(null);
    try {
      await fn();
      after?.();
      await load();
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card admin-card wide">
      <h2>{mcName}</h2>
      {err && <p className="err-text">{err}</p>}

      {/* Дерево */}
      <div className="sys-tree">
        {sites.length === 0 && <p className="muted">Нет объектов.</p>}
        {sites.map((s) => (
          <div key={s.id} className="sys-site">
            <div className="sys-site-head">
              {s.name} <span className="hint">({s.kind === 'complex' ? 'ЖК' : 'объект'}) · {s.address}</span>
            </div>
            {s.buildings.map((b) => (
              <div key={b.id} className="sys-building">
                🏢 {b.address}
                {b.entrances.length > 0 && (
                  <span className="hint"> — подъезды: {b.entrances.map((e) => e.number).join(', ')}</span>
                )}
              </div>
            ))}
          </div>
        ))}
      </div>

      <div className="sys-forms">
        <div className="form">
          <h3>Объект (ЖК)</h3>
          <input placeholder="Название" value={siteName} disabled={busy} onChange={(e) => setSiteName(e.target.value)} />
          <input placeholder="Адрес" value={siteAddr} disabled={busy} onChange={(e) => setSiteAddr(e.target.value)} />
          <select value={siteKind} disabled={busy} onChange={(e) => setSiteKind(e.target.value)}>
            <option value="complex">ЖК</option>
            <option value="standalone">Отдельный объект</option>
          </select>
          <button
            className="btn primary"
            disabled={busy || !siteName.trim() || !siteAddr.trim()}
            onClick={() => run(() => api.sysCreateSite(mcId, siteName.trim(), siteAddr.trim(), siteKind), () => { setSiteName(''); setSiteAddr(''); })}
          >
            Создать объект
          </button>
        </div>

        <div className="form">
          <h3>Дом</h3>
          <select value={bSite} disabled={busy || sites.length === 0} onChange={(e) => setBSite(e.target.value)}>
            <option value="">— объект —</option>
            {sites.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
          </select>
          <input placeholder="Адрес дома" value={bAddr} disabled={busy} onChange={(e) => setBAddr(e.target.value)} />
          <button
            className="btn primary"
            disabled={busy || !bSite || !bAddr.trim()}
            onClick={() => run(() => api.sysCreateBuilding(bSite, bAddr.trim()), () => setBAddr(''))}
          >
            Добавить дом
          </button>
        </div>

        <div className="form">
          <h3>Подъезд</h3>
          <select value={eBuilding} disabled={busy || buildings.length === 0} onChange={(e) => setEBuilding(e.target.value)}>
            <option value="">— дом —</option>
            {buildings.map((b) => <option key={b.id} value={b.id}>{b.siteName}: {b.address}</option>)}
          </select>
          <input placeholder="Номер подъезда" value={eNum} disabled={busy} onChange={(e) => setENum(e.target.value)} />
          <button
            className="btn primary"
            disabled={busy || !eBuilding || !eNum.trim()}
            onClick={() => run(() => api.sysCreateEntrance(eBuilding, eNum.trim()), () => setENum(''))}
          >
            Добавить подъезд
          </button>
        </div>

        <div className="form">
          <h3>Администратор УК</h3>
          <input placeholder="Email" value={aEmail} disabled={busy} onChange={(e) => setAEmail(e.target.value)} />
          <input placeholder="ФИО" value={aName} disabled={busy} onChange={(e) => setAName(e.target.value)} />
          <input placeholder="Пароль (≥8)" type="password" value={aPass} disabled={busy} onChange={(e) => setAPass(e.target.value)} />
          <button
            className="btn primary"
            disabled={busy || !aEmail.trim() || aPass.length < 8}
            onClick={() => run(
              async () => {
                const r = await api.sysCreateMCAdmin(mcId, aEmail.trim(), aName.trim(), aPass);
                setOtpauth(r.otpauth_url);
              },
              () => { setAEmail(''); setAName(''); setAPass(''); },
            )}
          >
            Создать УК-админа
          </button>
          {otpauth && (
            <div className="banner warning small">
              <p className="muted" style={{ margin: '0 0 6px' }}>2FA-ключ (показывается один раз — заведите в аутентификатор):</p>
              <code className="invite-url">{otpauth}</code>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export function SystemAdminPage() {
  const { logout } = useAuth();
  const nav = useNavigate();
  const [mcs, setMcs] = useState<SysMC[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const loadMCs = useCallback(async () => {
    setErr(null);
    try {
      const r = await api.sysListMCs();
      setMcs(r.management_companies);
    } catch (e) {
      setErr(mapErr(e));
    }
  }, []);

  useEffect(() => {
    void loadMCs();
  }, [loadMCs]);

  const doLogout = async () => {
    await logout();
    nav('/login', { replace: true });
  };

  const selectedMC = mcs.find((m) => m.id === selected) ?? null;

  return (
    <div className="page admin sys">
      <div className="admin-header header-top">
        <h1 style={{ margin: 0 }}>Платформа — управление</h1>
        <button className="btn ghost small" onClick={doLogout}>Выйти</button>
      </div>
      {err && <p className="err-text">{err}</p>}
      <div className="admin-grid sys-grid">
        <CompaniesPanel mcs={mcs} selected={selected} onSelect={setSelected} onCreated={loadMCs} />
        {selectedMC ? (
          <CompanyDetail key={selectedMC.id} mcId={selectedMC.id} mcName={selectedMC.name} />
        ) : (
          <div className="card admin-card"><p className="muted">Выберите УК слева.</p></div>
        )}
      </div>
    </div>
  );
}

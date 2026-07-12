import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ApiError, errorMessage } from '../api/client';
import { useAuth } from '../auth/AuthContext';

type Mode = 'resident' | 'admin';

function mapErr(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === 'UNAUTHORIZED') return 'Неверные данные или код.';
    if (e.code === 'RATE_LIMIT') return 'Слишком много попыток — попробуйте позже.';
    if (e.code === 'VALIDATION_ERROR') return 'Проверьте введённые данные.';
  }
  return errorMessage(e);
}

export function LoginPage() {
  const { otpSend, otpVerify, adminLogin } = useAuth();
  const nav = useNavigate();
  const [mode, setMode] = useState<Mode>('resident');
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Жилец
  const [phone, setPhone] = useState('+77010000001');
  const [code, setCode] = useState('');
  const [otpSent, setOtpSent] = useState(false);
  const [devCode, setDevCode] = useState<string | null>(null);

  // Админ
  const [email, setEmail] = useState('admin@demo.example');
  const [password, setPassword] = useState('');
  const [totp, setTotp] = useState('');
  const [adminDone, setAdminDone] = useState(false);

  const sendCode = async () => {
    setBusy(true);
    setErr(null);
    try {
      const r = await otpSend(phone.trim());
      setOtpSent(true);
      setDevCode(r.dev_code ?? null);
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  const verify = async () => {
    setBusy(true);
    setErr(null);
    try {
      await otpVerify(phone.trim(), code.trim());
      nav('/resident', { replace: true });
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  const doAdmin = async () => {
    setBusy(true);
    setErr(null);
    try {
      await adminLogin(email.trim(), password, totp.trim());
      setAdminDone(true);
    } catch (e) {
      setErr(mapErr(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="page center">
      <div className="card login-card">
        <h1>Вход</h1>

        <div className="tabs">
          <button
            className={`tab ${mode === 'resident' ? 'active' : ''}`}
            onClick={() => {
              setMode('resident');
              setErr(null);
            }}
          >
            Жилец
          </button>
          <button
            className={`tab ${mode === 'admin' ? 'active' : ''}`}
            onClick={() => {
              setMode('admin');
              setErr(null);
            }}
          >
            Администратор
          </button>
        </div>

        {mode === 'resident' ? (
          <div className="form">
            <label>
              Телефон
              <input
                type="tel"
                value={phone}
                disabled={otpSent || busy}
                onChange={(e) => setPhone(e.target.value)}
                placeholder="+7…"
              />
            </label>

            {!otpSent ? (
              <button className="btn success big" onClick={sendCode} disabled={busy}>
                {busy ? 'Отправка…' : 'Получить код'}
              </button>
            ) : (
              <>
                {devCode && (
                  <p className="hint">
                    dev-код: <code>{devCode}</code> (в проде придёт по SMS)
                  </p>
                )}
                <label>
                  Код из SMS
                  <input
                    type="text"
                    inputMode="numeric"
                    value={code}
                    disabled={busy}
                    onChange={(e) => setCode(e.target.value)}
                    placeholder="6 цифр"
                  />
                </label>
                <button className="btn success big" onClick={verify} disabled={busy || !code}>
                  {busy ? 'Проверка…' : 'Войти'}
                </button>
                <button
                  className="btn ghost"
                  onClick={() => {
                    setOtpSent(false);
                    setCode('');
                    setDevCode(null);
                    setErr(null);
                  }}
                  disabled={busy}
                >
                  Изменить телефон
                </button>
              </>
            )}
          </div>
        ) : adminDone ? (
          <div className="form">
            <p className="ok-text">Вход выполнен.</p>
            <p className="muted">
              Админ-API (устройства, аудит) доступен по токену. UI админки УК — вне скоупа этого
              инкремента.
            </p>
          </div>
        ) : (
          <div className="form">
            <label>
              Email
              <input
                type="email"
                value={email}
                disabled={busy}
                onChange={(e) => setEmail(e.target.value)}
              />
            </label>
            <label>
              Пароль
              <input
                type="password"
                value={password}
                disabled={busy}
                onChange={(e) => setPassword(e.target.value)}
              />
            </label>
            <label>
              Код 2FA (TOTP)
              <input
                type="text"
                inputMode="numeric"
                value={totp}
                disabled={busy}
                onChange={(e) => setTotp(e.target.value)}
                placeholder="6 цифр"
              />
            </label>
            <button
              className="btn success big"
              onClick={doAdmin}
              disabled={busy || !password || !totp}
            >
              {busy ? 'Проверка…' : 'Войти'}
            </button>
          </div>
        )}

        {err && <p className="err-text">{err}</p>}
      </div>
    </div>
  );
}

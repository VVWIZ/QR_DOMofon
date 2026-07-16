import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react';
import { api } from '../api/client';
import type { AuthUser, OtpSendResponse } from '../api/types';
import { clearAccessToken, setAccessToken, setOnUnauthorized } from './tokenStore';

type Status = 'loading' | 'authed' | 'anon';

interface AuthCtx {
  status: Status;
  user: AuthUser | null;
  isResident: boolean;
  isAdmin: boolean;
  isSystem: boolean;
  otpSend: (phone: string) => Promise<OtpSendResponse>;
  otpVerify: (phone: string, code: string) => Promise<void>;
  adminLogin: (email: string, password: string, totp: string) => Promise<AuthUser>;
  logout: () => Promise<void>;
}

const Ctx = createContext<AuthCtx | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Status>('loading');
  const [user, setUser] = useState<AuthUser | null>(null);

  // Когда refresh окончательно провалился (в client.ts) — переводим в anon.
  useEffect(() => {
    setOnUnauthorized(() => {
      setUser(null);
      setStatus('anon');
    });
    return () => setOnUnauthorized(null);
  }, []);

  // Bootstrap: тихий refresh по HttpOnly-cookie → /auth/me. Провал → anon.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const r = await api.authRefresh();
        setAccessToken(r.access_token);
        const me = await api.authMe();
        if (!cancelled) {
          setUser(me);
          setStatus('authed');
        }
      } catch {
        if (!cancelled) {
          clearAccessToken();
          setUser(null);
          setStatus('anon');
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const otpSend = useCallback((phone: string) => api.authOtpSend(phone), []);

  const otpVerify = useCallback(async (phone: string, code: string) => {
    const r = await api.authOtpVerify(phone, code);
    setAccessToken(r.access_token);
    setUser(r.user);
    setStatus('authed');
  }, []);

  const adminLogin = useCallback(async (email: string, password: string, totp: string) => {
    const r = await api.authAdminLogin(email, password, totp);
    setAccessToken(r.access_token);
    setUser(r.user);
    setStatus('authed');
    return r.user;
  }, []);

  const logout = useCallback(async () => {
    try {
      await api.authLogout();
    } catch {
      /* всё равно выходим локально */
    }
    clearAccessToken();
    setUser(null);
    setStatus('anon');
  }, []);

  const isResident = user?.kind === 'resident' || user?.kind === 'owner';
  const isAdmin = user?.kind === 'mc_admin';
  const isSystem = user?.kind === 'system_admin';

  return (
    <Ctx.Provider
      value={{ status, user, isResident, isAdmin, isSystem, otpSend, otpVerify, adminLogin, logout }}
    >
      {children}
    </Ctx.Provider>
  );
}

export function useAuth(): AuthCtx {
  const c = useContext(Ctx);
  if (!c) throw new Error('useAuth должен использоваться внутри AuthProvider');
  return c;
}

import type { ReactNode } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from '../auth/AuthContext';

/**
 * Гейт защищённых роутов. Пока идёт bootstrap — лоадер; аноним → /login;
 * requireResident=true и роль не resident/owner → /login; requireAdmin=true и
 * роль не mc_admin → /login. Резидентский UI жильца/владельца — в мобильном
 * приложении (вне веба); здесь на вебе — только УК-консоль (admin).
 */
export function ProtectedRoute({
  children,
  requireResident = false,
  requireAdmin = false,
  requireSystem = false,
}: {
  children: ReactNode;
  requireResident?: boolean;
  requireAdmin?: boolean;
  requireSystem?: boolean;
}) {
  const { status, isResident, isAdmin, isSystem } = useAuth();

  if (status === 'loading') {
    return (
      <div className="page center">
        <p className="muted">Проверка сессии…</p>
      </div>
    );
  }
  if (
    status === 'anon' ||
    (requireResident && !isResident) ||
    (requireAdmin && !isAdmin) ||
    (requireSystem && !isSystem)
  ) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}

import type { ReactNode } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from '../auth/AuthContext';

/**
 * Гейт защищённых роутов. Пока идёт bootstrap — лоадер; аноним → /login;
 * requireResident=true и роль не resident/owner → тоже /login (админ не имеет
 * UI жильца в этом инкременте).
 */
export function ProtectedRoute({
  children,
  requireResident = false,
}: {
  children: ReactNode;
  requireResident?: boolean;
}) {
  const { status, isResident } = useAuth();

  if (status === 'loading') {
    return (
      <div className="page center">
        <p className="muted">Проверка сессии…</p>
      </div>
    );
  }
  if (status === 'anon' || (requireResident && !isResident)) {
    return <Navigate to="/login" replace />;
  }
  return <>{children}</>;
}

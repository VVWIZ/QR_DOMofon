import { Link, Navigate, Route, Routes } from 'react-router-dom';
import { VisitorPage } from './pages/VisitorPage';
import { ResidentPage } from './pages/ResidentPage';
import { LoginPage } from './pages/LoginPage';
import { AdminPage } from './pages/AdminPage';
import { AuthProvider } from './auth/AuthContext';
import { ProtectedRoute } from './components/ProtectedRoute';

function Home() {
  return (
    <div className="page home">
      <div className="card">
        <h1>QR-Домофон</h1>
        <p className="muted">Демонстрационный walking skeleton.</p>
        <ul className="links">
          <li>
            <Link to="/login">Вход (жилец / администратор) — /login</Link>
          </li>
          <li>
            <Link to="/admin">УК-консоль — /admin</Link>
          </li>
          <li>
            <Link to="/resident">Экран жильца (демо-стенд-ин) — /resident</Link>
          </li>
        </ul>
        <p className="hint">
          Экран посетителя открывается по QR-ссылке вида{' '}
          <code>/v?aid=…&amp;v=1&amp;kid=…&amp;sig=…</code>
        </p>
      </div>
    </div>
  );
}

export function App() {
  return (
    <AuthProvider>
      <Routes>
        <Route path="/v" element={<VisitorPage />} />
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/resident"
          element={
            <ProtectedRoute requireResident>
              <ResidentPage />
            </ProtectedRoute>
          }
        />
        <Route
          path="/admin"
          element={
            <ProtectedRoute requireAdmin>
              <AdminPage />
            </ProtectedRoute>
          }
        />
        <Route path="/" element={<Home />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </AuthProvider>
  );
}

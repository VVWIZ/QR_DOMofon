import { Link, Navigate, Route, Routes } from 'react-router-dom';
import { VisitorPage } from './pages/VisitorPage';
import { ResidentPage } from './pages/ResidentPage';

function Home() {
  return (
    <div className="page home">
      <div className="card">
        <h1>QR-Домофон</h1>
        <p className="muted">Демонстрационный walking skeleton.</p>
        <ul className="links">
          <li>
            <Link to="/resident">Экран жильца — /resident</Link>
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
    <Routes>
      <Route path="/v" element={<VisitorPage />} />
      <Route path="/resident" element={<ResidentPage />} />
      <Route path="/" element={<Home />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

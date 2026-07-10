import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { App } from './App';
import './styles.css';

const container = document.getElementById('root');
if (!container) {
  throw new Error('Не найден корневой элемент #root');
}

// Без StrictMode намеренно: двойной mount/unmount в деве провоцирует лишний
// connect/disconnect LiveKit-комнаты. Cleanup-эффекты при этом корректны.
createRoot(container).render(
  <BrowserRouter>
    <App />
  </BrowserRouter>,
);

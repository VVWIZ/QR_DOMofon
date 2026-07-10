import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev-сервер на :5173. Прокси /api → backend :8080 снимает CORS в деве.
// SSE (/api/v1/resident/events) идёт через тот же прокси — http-proxy стримит
// text/event-stream по умолчанию, дополнительная настройка буферизации не нужна.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
});

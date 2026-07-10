import { useEffect, useRef } from 'react';

export type SSEStatus = 'connecting' | 'open' | 'closed';

/** eventName → обработчик распарсенного JSON-payload. */
export type SSEEventMap = Record<string, (data: unknown) => void>;

interface UseSSEOptions {
  events: SSEEventMap;
  onStatus?: (status: SSEStatus) => void;
  enabled?: boolean;
  /** Задержка реконнекта, мс. */
  retryMs?: number;
}

/**
 * Подписка на SSE-поток с именованными событиями и авто-reconnect.
 * Набор имён событий фиксируется на первом рендере (в нашем сценарии он статичен),
 * актуальные обработчики читаются через ref — без переподключения на каждый рендер.
 */
export function useSSE(path: string, { events, onStatus, enabled = true, retryMs = 2000 }: UseSSEOptions): void {
  const eventsRef = useRef(events);
  eventsRef.current = events;

  const onStatusRef = useRef(onStatus);
  onStatusRef.current = onStatus;

  // Имена событий захватываем один раз — стабильный список для addEventListener.
  const eventNamesRef = useRef<string[]>(Object.keys(events));

  useEffect(() => {
    if (!enabled) return;

    let source: EventSource | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;

    const connect = () => {
      onStatusRef.current?.('connecting');
      source = new EventSource(path);

      source.onopen = () => {
        onStatusRef.current?.('open');
      };

      source.onerror = () => {
        // EventSource переоткрывается сам при transient-ошибках; если соединение
        // закрыто окончательно — перезапускаем вручную с задержкой.
        if (source && source.readyState === EventSource.CLOSED) {
          source.close();
          source = null;
          onStatusRef.current?.('connecting');
          if (!stopped) {
            retryTimer = setTimeout(connect, retryMs);
          }
        }
      };

      for (const name of eventNamesRef.current) {
        source.addEventListener(name, (ev: MessageEvent) => {
          let data: unknown = null;
          if (ev.data) {
            try {
              data = JSON.parse(ev.data);
            } catch {
              data = ev.data;
            }
          }
          eventsRef.current[name]?.(data);
        });
      }
    };

    connect();

    return () => {
      stopped = true;
      if (retryTimer) clearTimeout(retryTimer);
      source?.close();
      onStatusRef.current?.('closed');
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, enabled, retryMs]);
}

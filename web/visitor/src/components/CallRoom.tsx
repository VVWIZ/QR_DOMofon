import { useEffect, useRef, useState } from 'react';
import {
  LocalTrackPublication,
  RemoteTrack,
  Room,
  RoomEvent,
  Track,
} from 'livekit-client';

export type CallRole = 'visitor' | 'resident';

interface CallRoomProps {
  url: string;
  token: string;
  /** visitor публикует camera+mic; resident — только mic (ТЗ §6.1). */
  role: CallRole;
  onDisconnected?: () => void;
  onError?: (err: Error) => void;
}

type ConnState = 'connecting' | 'connected' | 'error';

/**
 * Обёртка над livekit-client Room. Подключается к комнате по (url, token),
 * публикует треки по роли, рендерит видео и проигрывает удалённое аудио.
 * Корректно отключается при размонтировании.
 */
export function CallRoom({ url, token, role, onDisconnected, onError }: CallRoomProps) {
  const localVideoRef = useRef<HTMLVideoElement>(null);
  const remoteVideoRef = useRef<HTMLVideoElement>(null);
  const audioSinkRef = useRef<HTMLDivElement>(null);

  const [state, setState] = useState<ConnState>('connecting');
  const [remoteVideoActive, setRemoteVideoActive] = useState(false);

  useEffect(() => {
    let cancelled = false;
    const room = new Room({ adaptiveStream: true, dynacast: true });

    const handleTrackSubscribed = (track: RemoteTrack) => {
      if (track.kind === Track.Kind.Video) {
        if (remoteVideoRef.current) {
          track.attach(remoteVideoRef.current);
          setRemoteVideoActive(true);
        }
      } else if (track.kind === Track.Kind.Audio) {
        const el = track.attach();
        el.autoplay = true;
        audioSinkRef.current?.appendChild(el);
      }
    };

    const handleTrackUnsubscribed = (track: RemoteTrack) => {
      track.detach().forEach((el) => el.remove());
      if (track.kind === Track.Kind.Video) {
        setRemoteVideoActive(false);
      }
    };

    const handleLocalPublished = (pub: LocalTrackPublication) => {
      if (pub.track?.kind === Track.Kind.Video && localVideoRef.current) {
        pub.track.attach(localVideoRef.current);
      }
    };

    room
      .on(RoomEvent.TrackSubscribed, handleTrackSubscribed)
      .on(RoomEvent.TrackUnsubscribed, handleTrackUnsubscribed)
      .on(RoomEvent.LocalTrackPublished, handleLocalPublished)
      .on(RoomEvent.Disconnected, () => {
        if (!cancelled) onDisconnected?.();
      });

    (async () => {
      try {
        await room.connect(url, token);
        if (cancelled) return;

        // Микрофон публикуют обе роли.
        await room.localParticipant.setMicrophoneEnabled(true);
        // Камеру публикует только посетитель.
        if (role === 'visitor') {
          await room.localParticipant.setCameraEnabled(true);
        }

        if (!cancelled) setState('connected');
      } catch (err) {
        if (!cancelled) {
          setState('error');
          onError?.(err instanceof Error ? err : new Error(String(err)));
        }
      }
    })();

    return () => {
      cancelled = true;
      room.removeAllListeners();
      void room.disconnect();
    };
    // Переподключаемся только при смене комнаты/токена/роли.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, token, role]);

  return (
    <div className="call-room">
      <div className="video-stage">
        {role === 'resident' ? (
          <>
            <video ref={remoteVideoRef} className="stage-video" autoPlay playsInline />
            {!remoteVideoActive && (
              <div className="video-placeholder">Ожидание видео посетителя…</div>
            )}
          </>
        ) : (
          <>
            <video ref={localVideoRef} className="stage-video" autoPlay playsInline muted />
            <span className="self-label">Ваша камера</span>
          </>
        )}

        {state === 'connecting' && <div className="video-overlay">Подключение к комнате…</div>}
        {state === 'error' && (
          <div className="video-overlay error">
            Не удалось подключиться к медиа. Проверьте доступ к камере/микрофону.
          </div>
        )}
      </div>

      {/* Скрытый контейнер для удалённых аудио-элементов. */}
      <div ref={audioSinkRef} style={{ display: 'none' }} aria-hidden="true" />
    </div>
  );
}

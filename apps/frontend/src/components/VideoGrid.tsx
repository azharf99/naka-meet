import React, { useEffect, useRef, useState } from 'react';
import { ParticipantTrack } from '../services/webrtc';
import { MicOff, VideoOff, UserX, Loader2 } from 'lucide-react';

interface VideoGridProps {
  localStream: MediaStream | null;
  localScreenStream?: MediaStream | null;
  remoteTracks: ParticipantTrack[];
  displayName?: string;
  userRole?: string;
  remoteMediaState?: Map<string, { mic: boolean; cam: boolean }>;
  // Keyed the same way as remoteMediaState (by track.peerID, i.e. display
  // name — see the comment on ParticipantTrack.peerID). true while the
  // server's RTP-liveness watchdog has flagged that publisher as silent.
  remoteStaleState?: Map<string, boolean>;
  // Host-only "remove participant" action. Present only for the host role;
  // undefined elsewhere so VideoTile never renders the control at all.
  onRemoveParticipant?: (rawPeerId: string) => void;
}

export function getGridClass(totalTiles: number): string {
  if (totalTiles <= 1) {
    return 'grid-cols-1 max-w-4xl w-full mx-auto';
  }
  if (totalTiles === 2) {
    return 'grid-cols-1 md:grid-cols-2';
  }
  if (totalTiles <= 4) {
    return 'grid-cols-2 md:grid-cols-2';
  }
  if (totalTiles <= 9) {
    return 'grid-cols-2 md:grid-cols-3';
  }
  return 'grid-cols-2 md:grid-cols-4';
}

export function deduplicateTracks(remoteTracks: ParticipantTrack[]): ParticipantTrack[] {
  const map = new Map<string, ParticipantTrack>();
  for (const track of remoteTracks) {
    const key = track.stream?.id || track.peerID || track.id;
    const existing = map.get(key);
    const hasVideo = track.stream && typeof track.stream.getVideoTracks === 'function' && track.stream.getVideoTracks().length > 0;
    const existingNoVideo = existing && existing.stream && typeof existing.stream.getVideoTracks === 'function' && existing.stream.getVideoTracks().length === 0;
    if (!existing || track.isScreenShare || (hasVideo && existingNoVideo)) {
      map.set(key, track);
    }
  }
  return Array.from(map.values());
}

// The egress recorder page (rendered headlessly by Puppeteer and captured
// verbatim by FFmpeg — see App.tsx's `?role=egress` route) has no camera of
// its own (getUserMedia fails under Xvfb), so its local self-tile is never a
// real participant. Rendering it anyway produces an empty, permanently
// mic-muted "shadow" tile in every recording.
export function getTotalTileCount(remoteTrackCount: number, isEgress: boolean): number {
  return isEgress ? remoteTrackCount : remoteTrackCount + 1;
}

export function getInitials(name: string): string {
  const clean = name.replace(/\(.*\)/, '').trim();
  if (!clean) return 'U';
  const parts = clean.split(' ').filter(Boolean);
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[1][0]).toUpperCase();
}

export function deriveVisibility(
  isMicMuted: boolean | undefined,
  isCamOff: boolean | undefined,
  hasAudio: boolean,
  hasVideo: boolean,
  hasStream: boolean,
  isScreen: boolean
): { showVideoFallback: boolean; showMicMuted: boolean } {
  return {
    // An explicit signal (from the media_state relay) always wins over the
    // track-based heuristic, which can't observe a remote peer's own local
    // mute/cam-off toggle per WebRTC spec. Fall back to the heuristic only
    // when no signal has arrived yet (isCamOff/isMicMuted === undefined).
    showVideoFallback: !isScreen && (isCamOff === true || !hasStream || (isCamOff === undefined && !hasVideo)),
    showMicMuted: isMicMuted === true || (isMicMuted === undefined && !hasAudio),
  };
}

const VideoTile: React.FC<{
  stream?: MediaStream | null;
  label: string;
  isScreen?: boolean;
  isMicMuted?: boolean;
  isCamOff?: boolean;
  // True while the server's RTP-liveness watchdog has flagged this
  // publisher's track as silent — the video element still shows whatever
  // frame it last received (frozen), so this renders as an overlay rather
  // than replacing the tile, making clear *why* it looks stuck instead of
  // leaving a silently frozen frame with no explanation.
  isStale?: boolean;
  // Present only when the current user is this room's host and this tile
  // has a known raw participant ID to target — undefined otherwise, so the
  // control never renders for guests or for tiles metadata hasn't caught
  // up with yet.
  onRemove?: () => void;
}> = ({ stream, label, isScreen, isMicMuted, isCamOff, isStale, onRemove }) => {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [hasVideo, setHasVideo] = useState(false);
  const [hasAudio, setHasAudio] = useState(false);

  useEffect(() => {
    if (!stream) {
      setHasVideo(false);
      setHasAudio(false);
      return;
    }

    const checkTracks = () => {
      const vTrack = stream.getVideoTracks()[0];
      const aTrack = stream.getAudioTracks()[0];
      setHasVideo(!!(vTrack && vTrack.enabled && vTrack.readyState === 'live'));
      setHasAudio(!!(aTrack && aTrack.enabled && aTrack.readyState === 'live'));
    };

    checkTracks();

    if (videoRef.current && videoRef.current.srcObject !== stream) {
      videoRef.current.srcObject = stream;
    }

    const handleTrackEvent = () => {
      checkTracks();
      if (videoRef.current) {
        videoRef.current.srcObject = null;
        videoRef.current.srcObject = stream;
      }
    };

    stream.addEventListener('addtrack', handleTrackEvent);
    stream.addEventListener('removetrack', handleTrackEvent);

    const interval = setInterval(checkTracks, 1000);
    return () => {
      clearInterval(interval);
      stream.removeEventListener('addtrack', handleTrackEvent);
      stream.removeEventListener('removetrack', handleTrackEvent);
    };
  }, [stream]);

  const initials = getInitials(label);
  const { showVideoFallback, showMicMuted } = deriveVisibility(isMicMuted, isCamOff, hasAudio, hasVideo, !!stream, !!isScreen);

  return (
    <div className="group relative overflow-hidden rounded-2xl bg-slate-900 border border-slate-800 shadow-2xl flex items-center justify-center w-full h-full">
      <video
        ref={videoRef}
        autoPlay
        playsInline
        muted={label.includes('You')}
        className={`w-full h-full ${isScreen ? 'object-contain' : 'object-cover'} ${showVideoFallback ? 'hidden' : 'block'}`}
      />

      {/* Stale/frozen overlay: the video element above still shows whatever
          frame it last received, so this sits on top rather than replacing
          it — otherwise a frozen participant would silently look identical
          to a normal one until the auto-remove threshold quietly kicks
          them. */}
      {isStale && (
        <div className="absolute inset-0 bg-slate-950/70 backdrop-blur-[2px] flex flex-col items-center justify-center gap-2 text-amber-300 z-10 select-none">
          <Loader2 className="w-7 h-7 animate-spin" />
          <span className="text-xs font-medium tracking-wide">Reconnecting…</span>
        </div>
      )}

      {onRemove && (
        <button
          onClick={onRemove}
          title={`Remove ${label} from the meeting`}
          className="absolute top-3 left-3 z-20 p-1.5 bg-slate-950/80 hover:bg-red-600 text-slate-300 hover:text-white rounded-lg backdrop-blur-md border border-slate-800 hover:border-red-500/50 opacity-0 group-hover:opacity-100 transition-opacity shadow-md"
        >
          <UserX className="w-3.5 h-3.5" />
        </button>
      )}

      {showVideoFallback && (
        <div className="flex flex-col items-center justify-center gap-3 text-slate-400 p-6 select-none">
          <div className="w-20 h-20 rounded-full bg-slate-800 border-2 border-slate-700/80 flex items-center justify-center text-2xl font-bold text-slate-200 shadow-xl tracking-wider">
            {initials}
          </div>
          <span className="text-xs text-slate-300 font-medium tracking-wide">{label}</span>
        </div>
      )}


      {/* Floating Participant Tag */}
      <div className="absolute bottom-3 left-3 px-3 py-1 bg-slate-950/80 backdrop-blur-md rounded-lg text-xs font-medium text-slate-200 border border-slate-800 flex items-center gap-2">
        <span className={`w-2 h-2 rounded-full ${showVideoFallback ? 'bg-amber-500' : 'bg-emerald-500 animate-pulse'}`}></span>
        <span>{label}</span>
        {isScreen && ' (Screen)'}
      </div>

      {/* Red Mute Status Indicators (Zoom Style) */}
      <div className="absolute top-3 right-3 flex items-center gap-1.5 z-10">
        {showVideoFallback && (
          <div className="p-1.5 bg-red-500/80 text-white rounded-lg backdrop-blur-md border border-red-400/30 shadow-md" title="Camera Off / Device Unavailable">
            <VideoOff className="w-3.5 h-3.5" />
          </div>
        )}
        {showMicMuted && (
          <div className="p-1.5 bg-red-500/80 text-white rounded-lg backdrop-blur-md border border-red-400/30 shadow-md" title="Microphone Muted">
            <MicOff className="w-3.5 h-3.5" />
          </div>
        )}
      </div>
    </div>
  );
};

export const VideoGrid: React.FC<VideoGridProps> = ({
  localStream,
  localScreenStream,
  remoteTracks,
  displayName = 'You',
  userRole = 'host',
  remoteMediaState,
  remoteStaleState,
  onRemoveParticipant,
}) => {
  const uniqueTracks = deduplicateTracks(remoteTracks);
  const remoteScreenTrack = uniqueTracks.find((t) => t.isScreenShare);
  const isEgress = userRole === 'egress';
  const isHost = userRole === 'host';

  const activePresentationStream = localScreenStream || remoteScreenTrack?.stream;
  const presentationLabel = localScreenStream ? 'Your Screen Presentation' : 'Presentation Screen';
  // Only offer removal on the presentation tile when it's actually someone
  // else's screen — localScreenStream means it's the current user's own.
  const presentingRemoteTrack = !localScreenStream ? remoteScreenTrack : undefined;

  const localLabel = displayName ? `${displayName} (You)` : `You (${userRole})`;

  // Presenting doesn't exempt a participant from moderation, so both the
  // grid tiles and the Stage Mode presentation/sidebar tiles route through
  // this — undefined (not a no-op function) when the control shouldn't
  // render at all, matching VideoTile's onRemove prop contract.
  const removeHandlerFor = (track: ParticipantTrack): (() => void) | undefined =>
    isHost && track.rawPeerId ? () => onRemoveParticipant?.(track.rawPeerId) : undefined;

  // BR4: Stage Mode rendering when screen track or out-of-band metadata screen track is active.
  // Tiles fill their allotted space instead of scrolling: a fixed egress
  // viewport (Puppeteer recording this page) can never scroll, so anything
  // that overflowed here would simply be missing from the recording.
  if (activePresentationStream) {
    const sidebarTracks = uniqueTracks.filter((t) => !t.isScreenShare);
    return (
      <div className="flex-1 grid grid-cols-1 lg:grid-cols-4 gap-4 p-4 h-[calc(100vh-80px)] overflow-hidden">
        <div className="lg:col-span-3 h-full min-h-0">
          <VideoTile
            stream={activePresentationStream}
            label={presentationLabel}
            isScreen
            isStale={presentingRemoteTrack ? remoteStaleState?.get(presentingRemoteTrack.peerID) : undefined}
            onRemove={presentingRemoteTrack ? removeHandlerFor(presentingRemoteTrack) : undefined}
          />
        </div>
        <div className="flex flex-col gap-3 h-full min-h-0 overflow-hidden">
          {!isEgress && (
            <div className="flex-1 min-h-0">
              <VideoTile stream={localStream} label={localLabel} />
            </div>
          )}
          {sidebarTracks.map((track) => {
            // undefined (no signal received yet) must stay undefined, not
            // collapse to false, so VideoTile falls back to the track-based
            // heuristic instead of forcing "not muted"/"camera on".
            const state = remoteMediaState?.get(track.peerID);
            return (
              <div key={track.id} className="flex-1 min-h-0">
                <VideoTile
                  stream={track.stream}
                  label={`User ${track.peerID.slice(0, 6)}`}
                  isMicMuted={state ? state.mic === false : undefined}
                  isCamOff={state ? state.cam === false : undefined}
                  isStale={remoteStaleState?.get(track.peerID)}
                  onRemove={removeHandlerFor(track)}
                />
              </div>
            );
          })}
        </div>
      </div>
    );
  }

  const totalTiles = getTotalTileCount(uniqueTracks.length, isEgress);
  const gridClass = getGridClass(totalTiles);

  return (
    <div className="flex-1 p-6 h-[calc(100vh-80px)] flex items-center justify-center overflow-hidden">
      <div className={`grid ${gridClass} auto-rows-fr gap-4 w-full h-full justify-center items-stretch max-w-7xl`}>
        {!isEgress && <VideoTile stream={localStream} label={localLabel} />}
        {uniqueTracks.map((track) => {
          const state = remoteMediaState?.get(track.peerID);
          return (
            <VideoTile
              key={track.id}
              stream={track.stream}
              label={`User ${track.peerID.slice(0, 6)}`}
              isMicMuted={state ? state.mic === false : undefined}
              isCamOff={state ? state.cam === false : undefined}
              isStale={remoteStaleState?.get(track.peerID)}
              onRemove={removeHandlerFor(track)}
            />
          );
        })}
      </div>
    </div>
  );
};

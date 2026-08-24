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
  // This client's own raw participant ID — needed to tell whether
  // activePresentation (below) is this client's own screen share or
  // someone else's, since ParticipantTrack never carries an entry for the
  // local user's own tracks.
  localPeerId?: string;
  // The room's server-arbitrated active screen share (BR: when multiple
  // participants share at once, the latest is shown by default; others
  // suspended, not dropped; host can pick or reclaim). null/undefined
  // means nobody is presenting.
  activePresentation?: { peerId: string; peerName: string } | null;
  // Host-only screen-share arbitration override — pick any currently
  // (possibly suspended) presenter, including the host's own suspended
  // share, as the room's active presentation. Present only for the host
  // role, matching onRemoveParticipant's contract.
  onSetPresentation?: (peerId: string) => void;
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

// resolvePresentationView is the pure decision core of the screen-share
// arbitration UI (BR: the latest simultaneous share wins by default;
// others are suspended, not dropped; host can pick or reclaim). Pulled out
// of the VideoGrid component so the "who's on stage, who's suspended, is
// Stage Mode active at all" logic — the part most likely to hide a bug —
// has direct unit tests instead of needing to render the component.
export function resolvePresentationView(
  uniqueTracks: ParticipantTrack[],
  localPeerId: string | undefined,
  isPresentingLocally: boolean,
  activePeerId: string,
  activePeerName: string
): {
  activeIsLocal: boolean;
  activeRemoteTrack: ParticipantTrack | undefined;
  suspendedRemoteScreens: ParticipantTrack[];
  localSuspended: boolean;
  stageModeActive: boolean;
  presentationLabel: string;
} {
  const activeIsLocal = isPresentingLocally && !!localPeerId && activePeerId === localPeerId;
  const activeRemoteTrack = activeIsLocal
    ? undefined
    : uniqueTracks.find((t) => t.isScreenShare && t.rawPeerId === activePeerId);
  const suspendedRemoteScreens = uniqueTracks.filter(
    (t) => t.isScreenShare && t.rawPeerId !== activePeerId
  );
  const localSuspended = isPresentingLocally && !activeIsLocal;
  // Stage Mode triggers whenever anyone (including a suspended local
  // share) is presenting — not only when a stream can already be
  // resolved for it, since "I'm presenting but suspended" still needs the
  // Stage Mode layout to show that state rather than silently reverting
  // to the plain grid.
  const stageModeActive = !!activePeerId || isPresentingLocally;
  const presentationLabel = activeIsLocal
    ? 'Your Screen Presentation'
    : activeRemoteTrack
      ? `${activePeerName || 'Presenter'}'s Presentation`
      : 'Presentation Screen';

  return { activeIsLocal, activeRemoteTrack, suspendedRemoteScreens, localSuspended, stageModeActive, presentationLabel };
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
  // True for a screen share that's currently live but not the room's
  // active presentation (BR: the latest share wins by default; others are
  // suspended, not dropped, so the host can still choose to show them).
  isSuspended?: boolean;
  // Host-only "make this the active presentation" action for a suspended
  // share — present only for the host role, undefined for anyone else
  // (including for the presenter reclaiming their own suspended share:
  // that's a host-only action too, per BR).
  onPromote?: () => void;
}> = ({ stream, label, isScreen, isMicMuted, isCamOff, isStale, onRemove, isSuspended, onPromote }) => {
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

      {/* Suspended-presentation overlay: this share is still live, just not
          what the room is currently shown — persistent (not hover-only)
          since it's meaningful state, not a transient action affordance. */}
      {isSuspended && (
        <div className="absolute inset-0 bg-slate-950/60 flex flex-col items-center justify-center gap-2 z-10 select-none px-3 text-center">
          <span className="text-[11px] font-medium text-slate-300 tracking-wide">Presentation suspended</span>
          {onPromote && (
            <button
              onClick={onPromote}
              className="px-3 py-1.5 bg-indigo-600 hover:bg-indigo-500 text-white text-[11px] font-medium rounded-lg shadow-lg shadow-indigo-600/30 transition-colors"
            >
              Show to Everyone
            </button>
          )}
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
  localPeerId,
  activePresentation,
  onSetPresentation,
}) => {
  const uniqueTracks = deduplicateTracks(remoteTracks);
  const isEgress = userRole === 'egress';
  const isHost = userRole === 'host';

  // BR: multiple simultaneous screen shares are arbitrated server-side
  // (see RoomManager.SetScreenSharing / signaling's set_presentation) —
  // activePresentation names the one the whole room actually sees;
  // everything else live is "suspended," not dropped.
  const isPresentingLocally = !!localScreenStream;
  const activePeerId = activePresentation?.peerId || '';
  const {
    activeIsLocal,
    activeRemoteTrack,
    suspendedRemoteScreens,
    localSuspended,
    stageModeActive,
    presentationLabel,
  } = resolvePresentationView(uniqueTracks, localPeerId, isPresentingLocally, activePeerId, activePresentation?.peerName || '');
  const activePresentationStream = activeIsLocal ? localScreenStream : activeRemoteTrack?.stream;

  const localLabel = displayName ? `${displayName} (You)` : `You (${userRole})`;

  // Presenting doesn't exempt a participant from moderation, so both the
  // grid tiles and the Stage Mode presentation/sidebar tiles route through
  // this — undefined (not a no-op function) when the control shouldn't
  // render at all, matching VideoTile's onRemove prop contract.
  const removeHandlerFor = (track: ParticipantTrack): (() => void) | undefined =>
    isHost && track.rawPeerId ? () => onRemoveParticipant?.(track.rawPeerId) : undefined;

  // Host-only "show to everyone" for a suspended share — matches
  // removeHandlerFor's undefined-when-not-applicable contract.
  const promoteHandlerFor = (peerId: string): (() => void) | undefined =>
    isHost && peerId ? () => onSetPresentation?.(peerId) : undefined;

  // BR4/arbitration: Stage Mode rendering when a screen share (active or
  // suspended) exists anywhere in the room. Tiles fill their allotted
  // space instead of scrolling: a fixed egress viewport (Puppeteer
  // recording this page) can never scroll, so anything that overflowed
  // here would simply be missing from the recording.
  if (stageModeActive) {
    const sidebarCameraTracks = uniqueTracks.filter((t) => !t.isScreenShare);

    return (
      <div className="flex-1 grid grid-cols-1 lg:grid-cols-4 gap-4 p-4 h-[calc(100vh-80px)] overflow-hidden">
        <div className="lg:col-span-3 h-full min-h-0">
          <VideoTile
            stream={activePresentationStream}
            label={presentationLabel}
            isScreen
            isStale={activeRemoteTrack ? remoteStaleState?.get(activeRemoteTrack.peerID) : undefined}
            onRemove={activeRemoteTrack ? removeHandlerFor(activeRemoteTrack) : undefined}
          />
        </div>
        <div className="flex flex-col gap-3 h-full min-h-0 overflow-hidden">
          {!isEgress && (
            <div className="flex-1 min-h-0">
              <VideoTile stream={localStream} label={localLabel} />
            </div>
          )}

          {localSuspended && (
            <div className="flex-1 min-h-0">
              <VideoTile
                stream={localScreenStream}
                label="Your Screen"
                isScreen
                isSuspended
                onPromote={promoteHandlerFor(localPeerId || '')}
              />
            </div>
          )}

          {suspendedRemoteScreens.map((track) => (
            <div key={track.id} className="flex-1 min-h-0">
              <VideoTile
                stream={track.stream}
                label={`${track.peerID.slice(0, 12)}'s Screen`}
                isScreen
                isSuspended
                onPromote={promoteHandlerFor(track.rawPeerId)}
              />
            </div>
          ))}

          {sidebarCameraTracks.map((track) => {
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

import React, { useEffect, useRef, useState } from 'react';
import { ParticipantTrack } from '../services/webrtc';
import { MicOff, VideoOff } from 'lucide-react';

interface VideoGridProps {
  localStream: MediaStream | null;
  localScreenStream?: MediaStream | null;
  remoteTracks: ParticipantTrack[];
  displayName?: string;
  userRole?: string;
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

export function getInitials(name: string): string {
  const clean = name.replace(/\(.*\)/, '').trim();
  if (!clean) return 'U';
  const parts = clean.split(' ').filter(Boolean);
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[1][0]).toUpperCase();
}

const VideoTile: React.FC<{
  stream?: MediaStream | null;
  label: string;
  isScreen?: boolean;
  isMicMuted?: boolean;
}> = ({ stream, label, isScreen, isMicMuted }) => {
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
  const showVideoFallback = !isScreen && (!stream || !hasVideo);
  const showMicMuted = isMicMuted || !hasAudio;

  return (
    <div className={`relative overflow-hidden rounded-2xl bg-slate-900 border border-slate-800 shadow-2xl flex items-center justify-center ${isScreen ? 'w-full h-full min-h-[400px]' : 'w-full aspect-video min-h-[200px]'}`}>
      <video
        ref={videoRef}
        autoPlay
        playsInline
        muted={label.includes('You')}
        className={`w-full h-full ${isScreen ? 'object-contain' : 'object-cover'} ${showVideoFallback ? 'hidden' : 'block'}`}
      />
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
}) => {
  const uniqueTracks = deduplicateTracks(remoteTracks);
  const remoteScreenTrack = uniqueTracks.find((t) => t.isScreenShare);

  const activePresentationStream = localScreenStream || remoteScreenTrack?.stream;
  const presentationLabel = localScreenStream ? 'Your Screen Presentation' : 'Presentation Screen';

  const localLabel = displayName ? `${displayName} (You)` : `You (${userRole})`;

  // BR4: Stage Mode rendering when screen track or out-of-band metadata screen track is active
  if (activePresentationStream) {
    return (
      <div className="flex-1 grid grid-cols-1 lg:grid-cols-4 gap-4 p-4 h-[calc(100vh-80px)]">
        <div className="lg:col-span-3 h-full">
          <VideoTile stream={activePresentationStream} label={presentationLabel} isScreen />
        </div>
        <div className="flex flex-col gap-3 overflow-y-auto pr-1">
          <VideoTile stream={localStream} label={localLabel} />
          {uniqueTracks
            .filter((t) => !t.isScreenShare)
            .map((track) => (
              <VideoTile key={track.id} stream={track.stream} label={`User ${track.peerID.slice(0, 6)}`} />
            ))}
        </div>
      </div>
    );
  }

  const totalTiles = 1 + uniqueTracks.length; // Local participant tile is always rendered
  const gridClass = getGridClass(totalTiles);

  return (
    <div className="flex-1 p-6 overflow-y-auto h-[calc(100vh-80px)] flex items-center justify-center">
      <div className={`grid ${gridClass} gap-4 w-full justify-center items-center max-w-7xl`}>
        <VideoTile stream={localStream} label={localLabel} />
        {uniqueTracks.map((track) => (
          <VideoTile key={track.id} stream={track.stream} label={`User ${track.peerID.slice(0, 6)}`} />
        ))}
      </div>
    </div>
  );
};

import React, { useEffect, useRef } from 'react';
import { ParticipantTrack } from '../services/webrtc';

interface VideoGridProps {
  localStream: MediaStream | null;
  remoteTracks: ParticipantTrack[];
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
    if (!map.has(key) || track.isScreenShare) {
      map.set(key, track);
    }
  }
  return Array.from(map.values());
}

const VideoTile: React.FC<{ stream: MediaStream; label: string; isScreen?: boolean }> = ({
  stream,
  label,
  isScreen,
}) => {
  const videoRef = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    if (videoRef.current && stream) {
      videoRef.current.srcObject = stream;
    }
  }, [stream]);

  return (
    <div className={`relative overflow-hidden rounded-2xl bg-slate-900 border border-slate-800 shadow-2xl flex items-center justify-center ${isScreen ? 'w-full h-full' : 'w-full aspect-video min-h-[200px]'}`}>
      <video
        ref={videoRef}
        autoPlay
        playsInline
        muted={label.includes('You')}
        className={`w-full h-full ${isScreen ? 'object-contain' : 'object-cover'}`}
      />
      <div className="absolute bottom-3 left-3 px-3 py-1 bg-slate-950/80 backdrop-blur-md rounded-lg text-xs font-medium text-slate-200 border border-slate-800 flex items-center gap-2">
        <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></span>
        {label} {isScreen && '(Screen)'}
      </div>
    </div>
  );
};

export const VideoGrid: React.FC<VideoGridProps> = ({ localStream, remoteTracks }) => {
  const uniqueTracks = deduplicateTracks(remoteTracks);
  const screenTrack = uniqueTracks.find((t) => t.isScreenShare);

  if (screenTrack) {
    return (
      <div className="flex-1 grid grid-cols-1 lg:grid-cols-4 gap-4 p-4 h-[calc(100vh-80px)]">
        <div className="lg:col-span-3 h-full">
          <VideoTile stream={screenTrack.stream} label="Presentation Screen" isScreen />
        </div>
        <div className="flex flex-col gap-3 overflow-y-auto pr-1">
          {localStream && <VideoTile stream={localStream} label="You (Host)" />}
          {uniqueTracks
            .filter((t) => !t.isScreenShare)
            .map((track) => (
              <VideoTile key={track.id} stream={track.stream} label={`User ${track.peerID.slice(0, 6)}`} />
            ))}
        </div>
      </div>
    );
  }

  const totalTiles = (localStream ? 1 : 0) + uniqueTracks.length;
  const gridClass = getGridClass(totalTiles);

  return (
    <div className="flex-1 p-6 overflow-y-auto h-[calc(100vh-80px)] flex items-center justify-center">
      <div className={`grid ${gridClass} gap-4 w-full justify-center items-center max-w-7xl`}>
        {localStream && <VideoTile stream={localStream} label="You (Host)" />}
        {uniqueTracks.map((track) => (
          <VideoTile key={track.id} stream={track.stream} label={`User ${track.peerID.slice(0, 6)}`} />
        ))}
      </div>
    </div>
  );
};

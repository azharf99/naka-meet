import React, { useEffect, useRef } from 'react';
import { ParticipantTrack } from '../services/webrtc';

interface VideoGridProps {
  localStream: MediaStream | null;
  remoteTracks: ParticipantTrack[];
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
    <div className={`relative overflow-hidden rounded-xl bg-slate-900 border border-slate-800 shadow-2xl flex items-center justify-center ${isScreen ? 'w-full h-full' : 'aspect-video'}`}>
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
  // BR4: Cek apakah ada screen share track aktif
  const screenTrack = remoteTracks.find((t) => t.isScreenShare);

  if (screenTrack) {
    // Stage Mode (Presentation in Center + Picture-in-Picture Sidebar)
    return (
      <div className="flex-1 grid grid-cols-1 lg:grid-cols-4 gap-4 p-4 h-[calc(100vh-80px)]">
        {/* Main Presentation Stage */}
        <div className="lg:col-span-3 h-full">
          <VideoTile stream={screenTrack.stream} label="Presentation Screen" isScreen />
        </div>
        {/* Participant Sidebar */}
        <div className="flex flex-col gap-3 overflow-y-auto pr-1">
          {localStream && <VideoTile stream={localStream} label="You (Host)" />}
          {remoteTracks
            .filter((t) => !t.isScreenShare)
            .map((track) => (
              <VideoTile key={track.id} stream={track.stream} label={`User ${track.peerID.slice(0, 6)}`} />
            ))}
        </div>
      </div>
    );
  }

  // Grid Mode Default
  const totalTiles = (localStream ? 1 : 0) + remoteTracks.length;
  const gridCols = totalTiles <= 1 ? 'grid-cols-1' : totalTiles <= 4 ? 'grid-cols-2' : 'grid-cols-3';

  return (
    <div className={`flex-1 grid ${gridCols} gap-4 p-4 items-center justify-center overflow-y-auto h-[calc(100vh-80px)]`}>
      {localStream && <VideoTile stream={localStream} label="You (Host)" />}
      {remoteTracks.map((track) => (
        <VideoTile key={track.id} stream={track.stream} label={`User ${track.peerID.slice(0, 6)}`} />
      ))}
    </div>
  );
};

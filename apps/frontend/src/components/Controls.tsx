import React, { useState } from 'react';
import { Mic, MicOff, Video, VideoOff, Monitor, MessageSquare, Radio, PhoneOff } from 'lucide-react';

interface ControlsProps {
  userRole?: string;
  onToggleMic: () => void;
  onToggleCam: () => void;
  onScreenShare: () => void;
  onToggleChat: () => void;
  onTriggerEgress: () => void;
  onLeave: () => void;
}

export const Controls: React.FC<ControlsProps> = ({
  userRole = 'host',
  onToggleMic,
  onToggleCam,
  onScreenShare,
  onToggleChat,
  onTriggerEgress,
  onLeave,
}) => {
  const [micOn, setMicOn] = useState(true);
  const [camOn, setCamOn] = useState(true);
  const [isLiveStreaming, setIsLiveStreaming] = useState(false);

  const handleMic = () => {
    setMicOn(!micOn);
    onToggleMic();
  };

  const handleCam = () => {
    setCamOn(!camOn);
    onToggleCam();
  };

  const handleEgress = () => {
    setIsLiveStreaming(!isLiveStreaming);
    onTriggerEgress();
  };

  return (
    <div className="fixed bottom-4 left-1/2 -translate-x-1/2 glass-panel px-6 py-3 rounded-2xl flex items-center gap-4 z-50 border border-slate-700/50 shadow-2xl">
      {/* Audio Toggle */}
      <button
        onClick={handleMic}
        className={`p-3 rounded-xl transition-all duration-200 ${
          micOn ? 'bg-slate-800 hover:bg-slate-700 text-slate-200' : 'bg-red-500/20 text-red-400 border border-red-500/30'
        }`}
        title="Toggle Microphone"
      >
        {micOn ? <Mic className="w-5 h-5" /> : <MicOff className="w-5 h-5" />}
      </button>

      {/* Video Toggle */}
      <button
        onClick={handleCam}
        className={`p-3 rounded-xl transition-all duration-200 ${
          camOn ? 'bg-slate-800 hover:bg-slate-700 text-slate-200' : 'bg-red-500/20 text-red-400 border border-red-500/30'
        }`}
        title="Toggle Camera"
      >
        {camOn ? <Video className="w-5 h-5" /> : <VideoOff className="w-5 h-5" />}
      </button>

      {/* Screen Share */}
      <button
        onClick={onScreenShare}
        className="p-3 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-xl transition-all duration-200"
        title="Share Screen"
      >
        <Monitor className="w-5 h-5 text-indigo-400" />
      </button>

      {/* DataChannel Chat */}
      <button
        onClick={onToggleChat}
        className="p-3 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-xl transition-all duration-200"
        title="Real-Time Chat"
      >
        <MessageSquare className="w-5 h-5 text-cyan-400" />
      </button>

      {/* Host Only Egress Trigger (BR1) */}
      {userRole === 'host' && (
        <button
          onClick={handleEgress}
          className={`px-4 py-3 rounded-xl flex items-center gap-2 font-medium text-xs transition-all duration-200 ${
            isLiveStreaming ? 'bg-red-600 text-white animate-pulse' : 'bg-indigo-600 hover:bg-indigo-500 text-white'
          }`}
          title="Trigger Egress RTMP / Recording"
        >
          <Radio className="w-4 h-4" />
          {isLiveStreaming ? 'LIVE (Egress Active)' : 'Start Egress'}
        </button>
      )}

      {/* Leave Call */}
      <button
        onClick={onLeave}
        className="p-3 bg-red-600 hover:bg-red-500 text-white rounded-xl transition-all duration-200"
        title="Leave Room"
      >
        <PhoneOff className="w-5 h-5" />
      </button>
    </div>
  );
};


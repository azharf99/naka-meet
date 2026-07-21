import React, { useState } from 'react';
import { Mic, MicOff, Video, VideoOff, Monitor, MessageSquare, Radio, CircleDot, PhoneOff, X } from 'lucide-react';

export function validateRtmpUrl(url: string): boolean {
  if (!url) return false;
  return url.startsWith('rtmp://') || url.startsWith('rtmps://');
}

interface ControlsProps {
  userRole?: string;
  isRecording?: boolean;
  isLiveStreaming?: boolean;
  isScreenSharing?: boolean;
  onToggleMic: () => void;
  onToggleCam: () => void;
  onScreenShare: () => void;
  onStopScreenShare?: () => void;
  onToggleChat: () => void;
  onStartRecording: () => void;
  onStartRTMP: (url: string) => void;
  onStopEgress: () => void;
  onLeave: () => void;
}

export const Controls: React.FC<ControlsProps> = ({
  userRole = 'host',
  isRecording = false,
  isLiveStreaming = false,
  isScreenSharing = false,
  onToggleMic,
  onToggleCam,
  onScreenShare,
  onStopScreenShare,
  onToggleChat,
  onStartRecording,
  onStartRTMP,
  onStopEgress,
  onLeave,
}) => {
  const [micOn, setMicOn] = useState(true);
  const [camOn, setCamOn] = useState(true);
  const [showRtmpModal, setShowRtmpModal] = useState(false);
  const [rtmpUrlInput, setRtmpUrlInput] = useState('');
  const [rtmpError, setRtmpError] = useState('');

  const handleMic = () => {
    setMicOn(!micOn);
    onToggleMic();
  };

  const handleCam = () => {
    setCamOn(!camOn);
    onToggleCam();
  };

  const handleRtmpSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!validateRtmpUrl(rtmpUrlInput.trim())) {
      setRtmpError('URL RTMP harus diawali dengan rtmp:// atau rtmps://');
      return;
    }
    setRtmpError('');
    onStartRTMP(rtmpUrlInput.trim());
    setShowRtmpModal(false);
    setRtmpUrlInput('');
  };

  return (
    <>
      <div className="fixed bottom-4 left-1/2 -translate-x-1/2 glass-panel px-6 py-3 rounded-2xl flex items-center gap-3 z-50 border border-slate-700/50 shadow-2xl">
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
          onClick={isScreenSharing ? (onStopScreenShare || onScreenShare) : onScreenShare}
          className={`p-3 rounded-xl transition-all duration-200 flex items-center gap-2 ${
            isScreenSharing
              ? 'bg-indigo-600 hover:bg-indigo-500 text-white animate-pulse shadow-lg shadow-indigo-600/30'
              : 'bg-slate-800 hover:bg-slate-700 text-slate-200'
          }`}
          title={isScreenSharing ? 'Stop Sharing Screen' : 'Share Screen'}
        >
          <Monitor className={`w-5 h-5 ${isScreenSharing ? 'text-white' : 'text-indigo-400'}`} />
          {isScreenSharing && <span className="text-xs font-semibold pr-1">Stop Sharing</span>}
        </button>


        {/* DataChannel Chat */}
        <button
          onClick={onToggleChat}
          className="p-3 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-xl transition-all duration-200"
          title="Real-Time Chat"
        >
          <MessageSquare className="w-5 h-5 text-cyan-400" />
        </button>

        {/* Host Only Egress Controls */}
        {userRole === 'host' && (
          <div className="flex items-center gap-2 border-l border-slate-800 pl-3">
            {/* Record Room Button */}
            <button
              onClick={isRecording ? onStopEgress : onStartRecording}
              className={`px-3 py-2.5 rounded-xl flex items-center gap-2 font-medium text-xs transition-all duration-200 ${
                isRecording ? 'bg-red-600 text-white animate-pulse' : 'bg-slate-800 hover:bg-slate-700 text-red-400 border border-red-500/20'
              }`}
              title="Record Room to Persistent Storage"
            >
              <CircleDot className="w-4 h-4" />
              {isRecording ? 'REC Active' : 'Record'}
            </button>

            {/* Live Stream RTMP Button */}
            <button
              onClick={isLiveStreaming ? onStopEgress : () => setShowRtmpModal(true)}
              className={`px-3 py-2.5 rounded-xl flex items-center gap-2 font-medium text-xs transition-all duration-200 ${
                isLiveStreaming ? 'bg-indigo-600 text-white animate-pulse' : 'bg-slate-800 hover:bg-slate-700 text-indigo-400 border border-indigo-500/20'
              }`}
              title="Live Stream to YouTube RTMP"
            >
              <Radio className="w-4 h-4" />
              {isLiveStreaming ? 'LIVE Stream' : 'Go Live'}
            </button>
          </div>
        )}

        {/* Leave Call */}
        <button
          onClick={onLeave}
          className="p-3 bg-red-600 hover:bg-red-500 text-white rounded-xl transition-all duration-200 ml-2"
          title="Leave Room"
        >
          <PhoneOff className="w-5 h-5" />
        </button>
      </div>

      {/* RTMP Configuration Modal */}
      {showRtmpModal && (
        <div className="fixed inset-0 bg-slate-950/80 backdrop-blur-md z-50 flex items-center justify-center p-4">
          <div className="bg-slate-900 border border-slate-800 rounded-2xl p-6 w-full max-w-md shadow-2xl relative">
            <button
              onClick={() => setShowRtmpModal(false)}
              className="absolute top-4 right-4 text-slate-400 hover:text-slate-200"
            >
              <X className="w-5 h-5" />
            </button>

            <div className="flex items-center gap-3 mb-4">
              <div className="p-2.5 bg-indigo-500/10 border border-indigo-500/20 rounded-xl text-indigo-400">
                <Radio className="w-6 h-6" />
              </div>
              <div>
                <h3 className="font-semibold text-slate-100 text-base">Live Stream RTMP Setup</h3>
                <p className="text-xs text-slate-400">Masukkan YouTube / Twitch RTMP Ingestion URL</p>
              </div>
            </div>

            <form onSubmit={handleRtmpSubmit} className="space-y-4">
              <div>
                <label className="block text-xs font-medium text-slate-300 mb-1.5">
                  RTMP Ingestion URL & Stream Key
                </label>
                <input
                  type="text"
                  value={rtmpUrlInput}
                  onChange={(e) => setRtmpUrlInput(e.target.value)}
                  placeholder="rtmp://a.rtmp.youtube.com/live2/YOUR_STREAM_KEY"
                  className="w-full bg-slate-950 border border-slate-800 rounded-xl px-3.5 py-2.5 text-xs text-slate-100 focus:outline-none focus:border-indigo-500"
                  autoFocus
                />
                {rtmpError && <p className="text-[11px] text-red-400 mt-1.5">{rtmpError}</p>}
              </div>

              <div className="flex items-center justify-end gap-3 pt-2">
                <button
                  type="button"
                  onClick={() => setShowRtmpModal(false)}
                  className="px-4 py-2 text-xs font-medium text-slate-400 hover:text-slate-200 bg-slate-800 rounded-xl"
                >
                  Batal
                </button>
                <button
                  type="submit"
                  className="px-4 py-2 text-xs font-medium text-white bg-indigo-600 hover:bg-indigo-500 rounded-xl shadow-lg shadow-indigo-600/30"
                >
                  Start Live Stream
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </>
  );
};
